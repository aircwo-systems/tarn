package kms_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	kmshandler "github.com/aircwo-systems/tarn/internal/api/kms"
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/kms"
)

// decryptScript decrypts one message with the real AWS Encryption SDK, set up
// as Cognito's custom sender documentation shows. The keyring builds its own
// KMS client, which finds Tarn through AWS_ENDPOINT_URL.
const decryptScript = `
import { createRequire } from "node:module";
const [depsDir, keyArn, message] = process.argv.slice(2);
const require = createRequire(depsDir + "/package.json");
const { KmsKeyringNode, buildClient, CommitmentPolicy } = require("@aws-crypto/client-node");
const { decrypt } = buildClient(CommitmentPolicy.REQUIRE_ENCRYPT_ALLOW_DECRYPT);
const keyring = new KmsKeyringNode({ generatorKeyId: keyArn.split("/").pop(), keyIds: [keyArn] });
const { plaintext, messageHeader } = await decrypt(keyring, Buffer.from(message, "base64"));
console.log(JSON.stringify({ plaintext: Buffer.from(plaintext).toString("base64"), context: messageHeader.encryptionContext }));
`

// TestEncryptMessageDecryptsWithAWSEncryptionSDK is the golden test for the
// message encoder. It needs Node and a directory with @aws-crypto/client-node
// installed (npm i @aws-crypto/client-node), named by TARN_ESDK_NODE_DEPS.
func TestEncryptMessageDecryptsWithAWSEncryptionSDK(t *testing.T) {
	deps := os.Getenv("TARN_ESDK_NODE_DEPS")
	if deps == "" {
		t.Skip("set TARN_ESDK_NODE_DEPS to a directory with @aws-crypto/client-node installed")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Region = "eu-west-2"
	svc := kms.NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.Close)
	srv := httptest.NewServer(http.HandlerFunc(kmshandler.NewHandler(svc).Dispatch))
	t.Cleanup(srv.Close)

	key, err := svc.CreateKey(&kms.CreateKeyInput{Description: "golden"})
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "decrypt.mjs")
	if err := os.WriteFile(script, []byte(decryptScript), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := map[string]string{"userpool-id": "eu-west-2_golden"}

	for _, plaintext := range [][]byte{[]byte("123456"), bytes.Repeat([]byte("p"), 4096), bytes.Repeat([]byte("q"), 9000)} {
		msg, err := svc.EncryptMessage(key.KeyMetadata.Arn, plaintext, ctx)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, script, deps, key.KeyMetadata.Arn, base64.StdEncoding.EncodeToString(msg))
		cmd.Env = []string{
			"PATH=" + os.Getenv("PATH"),
			"AWS_ENDPOINT_URL=" + srv.URL,
			"AWS_REGION=" + cfg.Region,
			"AWS_ACCESS_KEY_ID=" + cfg.AccountID,
			"AWS_SECRET_ACCESS_KEY=test",
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("node decrypt of %d bytes: %v\n%s", len(plaintext), err, out)
		}
		var res struct {
			Plaintext []byte            `json:"plaintext"`
			Context   map[string]string `json:"context"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &res); err != nil {
			t.Fatalf("decode node output %q: %v", out, err)
		}
		if !bytes.Equal(res.Plaintext, plaintext) || res.Context["userpool-id"] != ctx["userpool-id"] {
			t.Fatalf("node decrypt of %d bytes = %d bytes, context %v", len(plaintext), len(res.Plaintext), res.Context)
		}
	}
}
