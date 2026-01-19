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

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"k8s.io/klog/v2"
)

var (
	projectID = flag.String("project_id", os.Getenv("GOOGLE_CLOUD_PROJECT"), "GCP Project ID in which to store the secret key.")
	keyName   = flag.String("key_name", "", "Name prefix for the created keys, '-secret' and '-public' suffixes will be added to the created Secret Manager resources.")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	if *projectID == "" {
		fmt.Fprintf(os.Stderr, "--project_id must be provided, or GOOGLE_CLOUD_PROJECT env var set.\n")
		os.Exit(1)
	}
	if *keyName == "" {
		fmt.Fprintf(os.Stderr, "--key_name must be provided.\n")
		os.Exit(1)
	}

	// Create a Secret Manager client.
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create Secret Manager client: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := client.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing secret manager client: %v", err)
		}
	}()

	// Create the key pair to store, and then store them.
	secPEM, pubPEM := genKeypairPEM()

	secKName := fmt.Sprintf("%s-secret", *keyName)
	if err := createSecret(ctx, *projectID, client, secKName, secPEM); err != nil {
		klog.Exitf("Failed to create secret %q: %v", secKName, err)
	}
	pubKName := fmt.Sprintf("%s-public", *keyName)
	if err := createSecret(ctx, *projectID, client, pubKName, pubPEM); err != nil {
		klog.Exitf("Failed to create secret %q: %v", pubKName, err)
	}

	// All done!
	fmt.Printf("Created new ECDSA keypair:\n  Secret name: %s\n  Public name: %v\n\nPublic Key:\n%s\n", secKName, pubKName, pubPEM)
}

func createSecret(ctx context.Context, projectID string, client *secretmanager.Client, name string, value []byte) error {
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
			Data: value,
		},
	}
	_, err = client.AddSecretVersion(ctx, addSecretVersionReq)
	return err
}

// genKeypairPEM generates an ECDSA key pair and returns PEM representations of
// the private and public keys encoded as ECPrivateKey and PKIX Public Key respectively.
func genKeypairPEM() ([]byte, []byte) {
	secK, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		klog.Exitf("Failed to generate key pair: %v", err)
	}

	secEC, err := x509.MarshalECPrivateKey(secK)
	if err != nil {
		klog.Exitf("Failed to marshal private key: %v", err)
	}
	secPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: secEC})

	pubK := secK.Public()
	pubPKIX, err := x509.MarshalPKIXPublicKey(pubK)
	if err != nil {
		klog.Exitf("Failed to marshal public key: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubPKIX})

	return secPEM, pubPEM
}
