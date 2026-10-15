package api

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
)

func TestLambdaLayerRoutes(t *testing.T) {
	for _, prefix := range []string{"/2018-10-31", "/2015-03-31"} {
		t.Run(prefix, func(t *testing.T) {
			h := newTestHTTPHandler(t)
			var archive bytes.Buffer
			zw := zip.NewWriter(&archive)
			file, err := zw.Create("chromium/marker.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.Write([]byte("layer route regression")); err != nil {
				t.Fatal(err)
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			payload := `{"CompatibleRuntimes":["nodejs20.x"],"Content":{"ZipFile":"` + base64.StdEncoding.EncodeToString(archive.Bytes()) + `"}}`
			versionsPath := prefix + "/layers/pdf-chromium/versions"
			published := compatibilityRequest(h, http.MethodPost, versionsPath, payload, "")
			if published.Code != http.StatusCreated {
				t.Fatalf("PublishLayerVersion status=%d body=%s", published.Code, published.Body)
			}
			var layer struct {
				LayerVersionArn string
				Version         int64
			}
			if err := json.Unmarshal(published.Body.Bytes(), &layer); err != nil {
				t.Fatal(err)
			}
			if layer.LayerVersionArn == "" || layer.Version != 1 {
				t.Fatalf("invalid published layer: %s", published.Body)
			}
			for _, path := range []string{prefix + "/layers", versionsPath, versionsPath + "/1"} {
				got := compatibilityRequest(h, http.MethodGet, path, "", "")
				if got.Code != http.StatusOK || !bytes.Contains(got.Body.Bytes(), []byte(layer.LayerVersionArn)) {
					t.Fatalf("GET %s status=%d body=%s", path, got.Code, got.Body)
				}
			}
			deleted := compatibilityRequest(h, http.MethodDelete, versionsPath+"/1", "", "")
			if deleted.Code != http.StatusNoContent {
				t.Fatalf("DeleteLayerVersion status=%d body=%s", deleted.Code, deleted.Body)
			}
			missing := compatibilityRequest(h, http.MethodGet, versionsPath+"/1", "", "")
			if missing.Code != http.StatusNotFound {
				t.Fatalf("deleted layer status=%d body=%s", missing.Code, missing.Body)
			}
		})
	}
}
