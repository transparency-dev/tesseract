// Copyright 2026 Google LLC. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// generate_key is a tool for generating an ECDSA key pair and storing it
// in GCP Secret Manager. This tool is intended to be used for creating
// signing keys for TesseraCT static CT logs.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"strings"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"golang.org/x/mod/sumdb/note"
)

const (
	kuLog     = "log"
	kuWitness = "witness"
)

var (
	projectID = flag.String("project_id", os.Getenv("GOOGLE_CLOUD_PROJECT"), "GCP Project ID in which to store the secret key.")
	origin    = flag.String("log_origin", "", "The origin of the log this key will be used with. The Secret Manager resource names will be derived from thiss tring, and have '{key_usage}-secret' and '{key_usage}-public' suffixes added.")
	keyUsage  = flag.String("key_usage", kuLog, "Type of key to create: '"+kuLog+"' or '"+kuWitness+"'. The created key names will include the key usage.")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	if *projectID == "" {
		exit("--project_id must be provided, or GOOGLE_CLOUD_PROJECT env var set.")
	}
	if *origin == "" {
		exit("--log_origin must be provided.\n")
	}

	// Create a Secret Manager client.
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		exit("Failed to create Secret Manager client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			exit("Error closing secret manager client: %v", err)
		}
	}()

	var sec, pub string

	switch ku := strings.ToLower(*keyUsage); ku {
	case kuLog:
		sec, pub = genECDSAKeypairPEM()
	case kuWitness:
		sec, pub = genEd25519KeypairNote()
	}

	pubKName := fmt.Sprintf("%s-%s-public", resourceFromOrigin(*origin), *keyUsage)
	if err := createSecret(ctx, *projectID, client, pubKName, pub); err != nil {
		exit("Failed to create secret %q: %v", pubKName, err)
	}
	secKName := fmt.Sprintf("%s-%s-secret", resourceFromOrigin(*origin), *keyUsage)
	if err := createSecret(ctx, *projectID, client, secKName, sec); err != nil {
		exit("Failed to create secret %q: %v", secKName, err)
	}

	// All done!
	fmt.Printf("Created new %s keypair:\n  Secret name: %s\n  Public name: %v\n\nPublic Key:\n%s\n", *keyUsage, secKName, pubKName, pub)
}

// resourceFromOrigin attempts to derive a safe GCP resource name from the provided origin string.
func resourceFromOrigin(o string) string {
	return strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			r == '-' {
			return r
		}
		return '-'
	}, o)
}

func createSecret(ctx context.Context, projectID string, client *secretmanager.Client, name string, value string) error {
	createSecretReq := &secretmanagerpb.CreateSecretRequest{
		Parent:   fmt.Sprintf("projects/%s", projectID),
		SecretId: name,
		Secret: &secretmanagerpb.Secret{
			Replication: &secretmanagerpb.Replication{
				Replication: &secretmanagerpb.Replication_Automatic_{
					Automatic: &secretmanagerpb.Replication_Automatic{},
				},
			},
		},
	}
	secret, err := client.CreateSecret(ctx, createSecretReq)
	if err != nil {
		return err
	}

	addSecretVersionReq := &secretmanagerpb.AddSecretVersionRequest{
		Parent: secret.Name,
		Payload: &secretmanagerpb.SecretPayload{
			Data: []byte(value),
		},
	}
	_, err = client.AddSecretVersion(ctx, addSecretVersionReq)
	return err
}

// genECDSAKeypairPEM generates an ECDSA key pair and returns PEM representations of
// the private and public keys encoded as ECPrivateKey and PKIX Public Key respectively.
func genECDSAKeypairPEM() (string, string) {
	secK, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		exit("Failed to generate key pair: %v", err)
	}

	secEC, err := x509.MarshalECPrivateKey(secK)
	if err != nil {
		exit("Failed to marshal private key: %v", err)
	}
	secPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: secEC})

	pubK := secK.Public()
	pubPKIX, err := x509.MarshalPKIXPublicKey(pubK)
	if err != nil {
		exit("Failed to marshal public key: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubPKIX})

	return string(secPEM), string(pubPEM)
}

// genEd25519KeypairNote generates an Ed25519 key pair and returns note vkey representations of
// the private and public keys respectively.
func genEd25519KeypairNote() (string, string) {
	skey, vkey, err := note.GenerateKey(rand.Reader, *origin)
	if err != nil {
		exit("Unable to create key: %q", err)
	}
	return skey, vkey
}

func exit(m string, args ...any) {
	fmt.Fprintf(os.Stderr, m+"\n", args...)
	os.Exit(1)
}
