// Copyright 2016 Google LLC. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// The ct_server binary runs the CT personality.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	aaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/dustin/go-humanize"
	"github.com/go-sql-driver/mysql"
	"github.com/transparency-dev/tessera"
	taws "github.com/transparency-dev/tessera/storage/aws"
	aws_as "github.com/transparency-dev/tessera/storage/aws/antispam"
	"github.com/transparency-dev/tesseract"
	"github.com/transparency-dev/tesseract/storage"
	"github.com/transparency-dev/tesseract/storage/aws"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/mod/sumdb/note"
	"k8s.io/klog/v2"
)

func init() {
	flag.Var(&notAfterStart, "not_after_start", "Start of the range of acceptable NotAfter values, inclusive. Leaving this unset or empty implies no lower bound to the range. RFC3339 UTC format, e.g: 2024-01-02T15:04:05Z.")
	flag.Var(&notAfterLimit, "not_after_limit", "Cut off point of notAfter dates - only notAfter dates strictly *before* notAfterLimit will be accepted. Leaving this unset or empty means no upper bound on the accepted range. RFC3339 UTC format, e.g: 2024-01-02T15:04:05Z.")
	flag.Float64Var(&dedupRL, "rate_limit_dedup", 100, "Rate limit for resolving duplicate submissions, in requests per second - i.e. duplicate requests for already integrated entries, which need to be fetched from the log storage by TesseraCT to extract their timestamp. When 0, all duplicate submissions are rejected. When negative, no rate limit is applied.")
	// DEPRECATED: will be removed shortly
	flag.Float64Var(&dedupRL, "pushback_max_dedupe_in_flight", 100, "DEPRECATED: use rate_limit_dedup. Maximum number of number of in-flight duplicate add requests - i.e. the number of requests matching entries that have already been integrated, but need to be fetched by the client to retrieve their timestamp. When 0, duplicate entries are always pushed back.")
}

// Global flags that affect all log instances.
var (
	notAfterStart timestampFlag
	notAfterLimit timestampFlag
	dedupRL       float64

	// Functionality flags
	httpEndpoint             = flag.String("http_endpoint", "localhost:6962", "Endpoint for HTTP (host:port).")
	maskInternalErrors       = flag.Bool("mask_internal_errors", false, "Don't return error strings with Internal Server Error HTTP responses.")
	origin                   = flag.String("origin", "", "Origin of the log, for checkpoints. This MUST match the log's submission prefix as per https://c2sp.org/static-ct-api.")
	pathPrefix               = flag.String("path_prefix", "", "Prefix to use on endpoints URL paths: HOST:PATH_PREFIX/ct/v1/ENDPOINT.")
	rootsPemFile             = flag.String("roots_pem_file", "", "Path to the file containing root certificates that are acceptable to the log. The certs are served through get-roots endpoint.")
	rootsRemoteFetchURL      = flag.String("roots_remote_fetch_url", "https://ccadb.my.salesforce-sites.com/ccadb/RootCACertificatesIncludedByRSReportCSV", "WIP DO NOT USE - URL to fetch trusted roots from.")
	rootsRemoteFetchInterval = flag.Duration("roots_remote_fetch_interval", time.Duration(0), "WIP DO NOT USE - Interval between two fetches from roots_fetch_url.")
	rejectExpired            = flag.Bool("reject_expired", false, "If true then the certificate validity period will be checked against the current time during the validation of submissions. This will cause expired certificates to be rejected.")
	rejectUnexpired          = flag.Bool("reject_unexpired", false, "If true then TesseraCT rejects certificates that are either currently valid or not yet valid.")
	extKeyUsages             = flag.String("ext_key_usages", "", "If set, will restrict the set of such usages that the server will accept. By default all are accepted. The values specified must be ones known to the x509 package.")
	rejectExtensions         = flag.String("reject_extension", "", "A list of X.509 extension OIDs, in dotted string form (e.g. '2.3.4.5') which, if present, should cause submissions to be rejected.")
	acceptSHA1               = flag.Bool("accept_sha1_signing_algorithms", true, "If true, accept chains that use SHA-1 based signing algorithms. This flag will eventually be removed, and such algorithms will be rejected.")
	enablePublicationAwaiter = flag.Bool("enable_publication_awaiter", true, "If true then the certificate is integrated into log before returning the response.")
	witnessPolicyFile        = flag.String("witness_policy_file", "", "(Optional) Path to the file containing the witness policy in the format described at https://git.glasklar.is/sigsum/core/sigsum-go/-/blob/main/doc/policy.md")
	witnessTimeout           = flag.Duration("witness_timeout", tessera.DefaultWitnessTimeout, "Maximum time to wait for witness responses.")
	notBeforeRL              = flag.String("rate_limit_old_not_before", "", "Optionally rate limits submissions with old notBefore dates. Expects a value of with the format: \"<go duration>:<rate limit>\", e.g. \"30d:50\" would impose a limit of 50 certs/s on submissions whose notBefore date is >= 30days old.")

	// Performance flags
	httpDeadline                = flag.Duration("http_deadline", time.Second*10, "Deadline for HTTP requests.")
	inMemoryAntispamCacheSize   = flag.String("inmemory_antispam_cache_size", "256k", "Maximum number of entries to keep in the in-memory antispam cache. Unitless with SI metric prefixes, such as '256k'.")
	checkpointInterval          = flag.Duration("checkpoint_interval", 1500*time.Millisecond, "Interval between publishing checkpoints when the log has grown")
	checkpointRepublishInterval = flag.Duration("checkpoint_republish_interval", 30*time.Second, "Interval between republishing a checkpoint for a log which hasn't grown since the previous checkpoint was published")
	batchMaxSize                = flag.Uint("batch_max_size", tessera.DefaultBatchMaxSize, "Maximum number of entries to process in a single Tessera sequencing batch.")
	batchMaxAge                 = flag.Duration("batch_max_age", tessera.DefaultBatchMaxAge, "Maximum age of entries in a single Tessera sequencing batch.")
	pushbackMaxOutstanding      = flag.Uint("pushback_max_outstanding", tessera.DefaultPushbackMaxOutstanding, "Maximum number of number of in-flight add requests - i.e. the number of entries with sequence numbers assigned, but which are not yet integrated into the log.")
	pushbackMaxAntispamLag      = flag.Uint("pushback_max_antispam_lag", aws_as.DefaultPushbackThreshold, "Maximum permitted lag for antispam follower, before log starts returneing pushback.")
	garbageCollectionInterval   = flag.Duration("garbage_collection_interval", 10*time.Second, "Interval between scans to remove obsolete partial tiles and entry bundles. Set to 0 to disable.")

	// Infrastructure setup flags
	bucket                     = flag.String("bucket", "", "Name of the S3 bucket to store the log in.")
	dbName                     = flag.String("db_name", "", "AuroraDB name")
	antispamDBName             = flag.String("antispam_db_name", "", "AuroraDB antispam name")
	dbHost                     = flag.String("db_host", "", "AuroraDB host")
	dbPort                     = flag.Int("db_port", 3306, "AuroraDB port")
	dbUser                     = flag.String("db_user", "", "AuroraDB user")
	dbPassword                 = flag.String("db_password", "", "AuroraDB password")
	dbMaxConns                 = flag.Int("db_max_conns", 0, "Maximum connections to the database, defaults to 0, i.e unlimited")
	dbMaxIdle                  = flag.Int("db_max_idle_conns", 2, "Maximum idle database connections in the connection pool, defaults to 2")
	signerPublicKeySecretName  = flag.String("signer_public_key_secret_name", "", "Public key secret name for checkpoints and SCTs signer")
	signerPrivateKeySecretName = flag.String("signer_private_key_secret_name", "", "Private key secret name for checkpoints and SCTs signer")
	signerPublicKeyFile        = flag.String("signer_public_key_file", "", "Path to public key file for checkpoints and SCTs signer (alternative to secrets manager)")
	signerPrivateKeyFile       = flag.String("signer_private_key_file", "", "Path to private key file for checkpoints and SCTs signer (alternative to secrets manager)")
	usePathStyle               = flag.Bool("s3_use_path_style", false, "Whether to force the AWS S3 client to use path-style bucket references, probably only useful for on-prem deployments")
)

// nolint:staticcheck
func main() {
	klog.InitFlags(nil)
	flag.Parse()
	ctx := context.Background()

	var signer *ECDSAWithSHA256Signer
	var err error

	// Check if local key files are specified
	if *signerPublicKeyFile != "" && *signerPrivateKeyFile != "" {
		signer, err = NewLocalSigner(*signerPublicKeyFile, *signerPrivateKeyFile)
		if err != nil {
			klog.Exitf("Can't create local file signer: %v", err)
		}
	} else if *signerPublicKeySecretName != "" && *signerPrivateKeySecretName != "" {
		signer, err = NewSecretsManagerSigner(ctx, *signerPublicKeySecretName, *signerPrivateKeySecretName)
		if err != nil {
			klog.Exitf("Can't create AWS Secrets Manager signer: %v", err)
		}
	} else {
		klog.Exit("Must specify either local key files (--signer_public_key_file and --signer_private_key_file) or secrets manager keys (--signer_public_key_secret_name and --signer_private_key_secret_name)")
	}

	chainValidationConfig := tesseract.ChainValidationConfig{
		RootsPEMFile:             *rootsPemFile,
		RootsRemoteFetchURL:      *rootsRemoteFetchURL,
		RootsRemoteFetchInterval: *rootsRemoteFetchInterval,
		RejectExpired:            *rejectExpired,
		RejectUnexpired:          *rejectUnexpired,
		ExtKeyUsages:             *extKeyUsages,
		RejectExtensions:         *rejectExtensions,
		NotAfterStart:            notAfterStart.t,
		NotAfterLimit:            notAfterLimit.t,
		AcceptSHA1:               *acceptSHA1,
	}
	if *acceptSHA1 {
		klog.Info(`**** WARNING **** This server will accept chains signed
using SHA-1 based algorithms. This feature is available to allow chains
submitted by Chrome's Merge Delay Monitor Root for the time being, but will
eventually go away. See /internal/lax509/README.md for more information.`)
	}

	hOpts := tesseract.LogHandlerOpts{
		NotBeforeRL: notBeforeRLFromFlags(),
		DedupRL:     dedupRL,
	}
	logHandler, err := tesseract.NewLogHandler(ctx, *origin, signer, chainValidationConfig, newAWSStorage, *httpDeadline, *maskInternalErrors, *pathPrefix, hOpts)
	if err != nil {
		klog.Exitf("Can't initialize CT HTTP Server: %v", err)
	}

	klog.CopyStandardLogTo("WARNING")
	klog.Info("**** CT HTTP Server Starting ****")
	http.Handle("/", otelhttp.NewHandler(logHandler, "/"))

	// Bring up the HTTP server and serve until we get a signal not to.
	srv := http.Server{
		Addr: *httpEndpoint,
		// Set timeout for reading headers to avoid a slowloris attack.
		ReadHeaderTimeout: 5 * time.Second,
	}
	shutdownWG := new(sync.WaitGroup)
	shutdownWG.Add(1)
	go awaitSignal(func() {
		defer shutdownWG.Done()
		// Allow 60s for any pending requests to finish then terminate any stragglers
		// TODO(phboneff): maybe wait for the sequencer queue to be empty?
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*60)
		defer cancel()
		klog.Info("Shutting down HTTP server...")
		if err := srv.Shutdown(ctx); err != nil {
			klog.Errorf("srv.Shutdown(): %v", err)
		}
		klog.Info("HTTP server shutdown")
	})

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		klog.Warningf("Server exited: %v", err)
	}
	// Wait will only block if the function passed to awaitSignal was called,
	// in which case it'll block until the HTTP server has gracefully shutdown
	shutdownWG.Wait()
	klog.Flush()
}

// awaitSignal waits for standard termination signals, then runs the given
// function; it should be run as a separate goroutine.
func awaitSignal(doneFn func()) {
	// Arrange notification for the standard set of signals used to terminate a server
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// Now block main and wait for a signal
	sig := <-sigs
	klog.Warningf("Signal received: %v", sig)
	klog.Flush()

	doneFn()
}

func newAWSStorage(ctx context.Context, signer note.Signer) (*storage.CTStorage, error) {
	awsCfg := storageConfigFromFlags()
	driver, err := taws.New(ctx, awsCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize AWS Tessera storage driver: %v", err)
	}

	var antispam tessera.Antispam
	if *antispamDBName != "" {
		antispam, err = aws_as.NewAntispam(ctx, antispamMySQLConfig().FormatDSN(), aws_as.AntispamOpts{PushbackThreshold: *pushbackMaxAntispamLag})
		if err != nil {
			klog.Exitf("Failed to create new AWS antispam storage: %v", err)
		}
	}

	antispamCacheSize, unit, error := humanize.ParseSI(*inMemoryAntispamCacheSize)
	if unit != "" {
		return nil, fmt.Errorf("invalid antispam cache size, used unit %q, want none", unit)
	}
	if error != nil {
		return nil, fmt.Errorf("invalid antispam cache size: %v", error)
	}

	opts := tessera.NewAppendOptions().
		WithCheckpointSigner(signer).
		WithCTLayout().
		WithAntispam(uint(antispamCacheSize), antispam).
		WithCheckpointInterval(*checkpointInterval).
		WithCheckpointRepublishInterval(*checkpointRepublishInterval).
		WithBatching(*batchMaxSize, *batchMaxAge).
		WithPushback(*pushbackMaxOutstanding).
		WithGarbageCollectionInterval(*garbageCollectionInterval)

	if *witnessPolicyFile != "" {
		f, err := os.ReadFile(*witnessPolicyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read witness policy file %q: %v", *witnessPolicyFile, err)
		}
		wg, err := tessera.NewWitnessGroupFromPolicy(f)
		if err != nil {
			return nil, fmt.Errorf("failed to create witness group from policy: %v", err)
		}

		// Don't block if witnesses are unavailable.
		wOpts := &tessera.WitnessOptions{
			FailOpen: true,
			Timeout:  *witnessTimeout,
		}
		opts.WithWitnesses(wg, wOpts)
	}

	appender, _, reader, err := tessera.NewAppender(ctx, driver, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize AWS Tessera storage: %v", err)
	}

	issuerStorage, err := aws.NewIssuerStorage(ctx, aws.Options{
		Bucket:    *bucket,
		SDKConfig: awsCfg.SDKConfig,
		S3Options: awsCfg.S3Options,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize AWS issuer storage: %v", err)
	}

	sopts := storage.CTStorageOptions{
		Appender:      appender,
		Reader:        reader,
		IssuerStorage: issuerStorage,
		EnableAwaiter: *enablePublicationAwaiter,
	}

	return storage.NewCTStorage(ctx, &sopts)
}

type timestampFlag struct {
	t *time.Time
}

func (t *timestampFlag) String() string {
	if t.t != nil {
		return t.t.Format(time.RFC3339)
	}
	return ""
}

func (t *timestampFlag) Set(w string) error {
	if w == "" {
		return nil
	} else if !strings.HasSuffix(w, "Z") {
		return fmt.Errorf("timestamps MUST be in UTC, got %v", w)
	}
	tt, err := time.Parse(time.RFC3339, w)
	if err != nil {
		return fmt.Errorf("can't parse %q as RFC3339 timestamp: %v", w, err)
	}
	t.t = &tt
	return nil
}

// storageConfigFromFlags returns an aws.Config struct populated with values
// provided via flags.
func storageConfigFromFlags() taws.Config {
	if *bucket == "" {
		klog.Exit("--bucket must be set")
	}
	if *dbName == "" {
		klog.Exit("--db_name must be set")
	}
	if *dbHost == "" {
		klog.Exit("--db_host must be set")
	}
	if *dbPort == 0 {
		klog.Exit("--db_port must be set")
	}
	if *dbUser == "" {
		klog.Exit("--db_user must be set")
	}
	// Empty password isn't an option with AuroraDB MySQL.
	if *dbPassword == "" {
		klog.Exit("--db_password must be set")
	}

	c := mysql.Config{
		User:                    *dbUser,
		Passwd:                  *dbPassword,
		Net:                     "tcp",
		Addr:                    fmt.Sprintf("%s:%d", *dbHost, *dbPort),
		DBName:                  *dbName,
		AllowCleartextPasswords: true,
		AllowNativePasswords:    true,
	}

	var s3Opts func(o *s3.Options)
	var awsConfig *aaws.Config

	if *usePathStyle {
		s3Opts = func(o *s3.Options) {
			o.UsePathStyle = true
			o.BaseEndpoint = aaws.String(os.Getenv("AWS_ENDPOINT_URL_S3"))
			o.Credentials = credentials.NewStaticCredentialsProvider(os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"), "")
		}
		awsConfig = &aaws.Config{
			Region: os.Getenv("AWS_DEFAULT_REGION"),
		}
	}
	return taws.Config{
		Bucket:       *bucket,
		DSN:          c.FormatDSN(),
		MaxOpenConns: *dbMaxConns,
		MaxIdleConns: *dbMaxIdle,
		SDKConfig:    awsConfig,
		S3Options:    s3Opts,
	}
}

func antispamMySQLConfig() *mysql.Config {
	if *antispamDBName == "" {
		klog.Exit("--antispam_db_name must be set")
	}
	if *dbHost == "" {
		klog.Exit("--db_host must be set")
	}
	if *dbPort == 0 {
		klog.Exit("--db_port must be set")
	}
	if *dbUser == "" {
		klog.Exit("--db_user must be set")
	}
	// Empty passord isn't an option with AuroraDB MySQL.
	if *dbPassword == "" {
		klog.Exit("--db_password must be set")
	}

	return &mysql.Config{
		User:                    *dbUser,
		Passwd:                  *dbPassword,
		Net:                     "tcp",
		Addr:                    fmt.Sprintf("%s:%d", *dbHost, *dbPort),
		DBName:                  *antispamDBName,
		AllowCleartextPasswords: true,
		AllowNativePasswords:    true,
	}
}

func notBeforeRLFromFlags() *tesseract.NotBeforeRL {
	if *notBeforeRL == "" {
		return nil
	}
	bits := strings.Split(*notBeforeRL, ":")
	if len(bits) != 2 {
		klog.Exitf("Invalid format for --rate_limit_old_not_before flag")
	}
	a, err := time.ParseDuration(bits[0])
	if err != nil {
		klog.Exitf("Invalid age passed to --rate_limit_old_not_before flag %q: %v", bits[0], err)
	}
	l, err := strconv.ParseFloat(bits[1], 64)
	if err != nil {
		klog.Exitf("Invalid rate limit passed to --rate_limit_old_not_before %q: %v", bits[1], err)
	}
	return &tesseract.NotBeforeRL{AgeThreshold: a, RateLimit: l}
}
