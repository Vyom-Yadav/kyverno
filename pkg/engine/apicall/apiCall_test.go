package apicall

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	enginecontext "github.com/kyverno/kyverno/pkg/engine/context"
	"gotest.tools/assert"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func buildTestServer(responseData []byte) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/resource", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Write(responseData)
			return
		}

		if r.Method == "POST" {
			defer r.Body.Close()
			body, _ := io.ReadAll(r.Body)
			w.Write(body)
		}
	})

	return httptest.NewServer(mux)
}

func Test_serviceGetRequest(t *testing.T) {
	serverResponse := []byte(`{ "day": "Sunday" }`)
	s := buildTestServer(serverResponse)
	defer s.Close()

	entry := kyvernov1.ContextEntry{}
	ctx := enginecontext.NewContext()

	_, err := New(context.TODO(), entry, ctx, nil, logr.Discard())
	assert.ErrorContains(t, err, "missing APICall")

	entry.Name = "test"
	entry.APICall = &kyvernov1.APICall{
		Service: &kyvernov1.ServiceCall{
			URL: s.URL,
		},
	}

	call, err := New(context.TODO(), entry, ctx, nil, logr.Discard())
	assert.NilError(t, err)
	_, err = call.Execute()
	assert.ErrorContains(t, err, "invalid request type")

	entry.APICall.Service.Method = "GET"
	call, err = New(context.TODO(), entry, ctx, nil, logr.Discard())
	assert.NilError(t, err)
	_, err = call.Execute()
	assert.ErrorContains(t, err, "HTTP 404")

	entry.APICall.Service.URL = s.URL + "/resource"
	call, err = New(context.TODO(), entry, ctx, nil, logr.Discard())
	assert.NilError(t, err)

	data, err := call.Execute()
	assert.NilError(t, err)
	assert.Assert(t, data != nil, "nil data")
	assert.Equal(t, string(serverResponse), string(data))
}

func Test_servicePostRequest(t *testing.T) {
	serverResponse := []byte(`{ "day": "Monday" }`)
	s := buildTestServer(serverResponse)
	defer s.Close()

	entry := kyvernov1.ContextEntry{
		Name: "test",
		APICall: &kyvernov1.APICall{
			Service: &kyvernov1.ServiceCall{
				URL:    s.URL + "/resource",
				Method: "POST",
			},
		},
	}

	ctx := enginecontext.NewContext()
	call, err := New(context.TODO(), entry, ctx, nil, logr.Discard())
	assert.NilError(t, err)
	data, err := call.Execute()
	assert.NilError(t, err)
	assert.Equal(t, "{}\n", string(data))

	imageData := `{
		"containers": {
		  "tomcat": {
			"reference": "https://ghcr.io/tomcat/tomcat:9",
			"registry": "https://ghcr.io",
			"path": "tomcat",
			"name": "tomcat",
			"tag": "9"
		  }
		},
		"initContainers": {
		  "vault": {
			"reference": "https://ghcr.io/vault/vault:v3",
			"registry": "https://ghcr.io",
			"path": "vault",
			"name": "vault",
			"tag": "v3"
		  }
		},
		"ephemeralContainers": {
			"vault": {
			  "reference": "https://ghcr.io/busybox/busybox:latest",
			  "registry": "https://ghcr.io",
			  "path": "busybox",
			  "name": "busybox",
			  "tag": "latest"
			}
		  }		
	  }`

	err = ctx.AddContextEntry("images", []byte(imageData))
	assert.NilError(t, err)

	entry.APICall.Service.Data = []kyvernov1.RequestData{
		{
			Key: "images",
			Value: &apiextensionsv1.JSON{
				Raw: []byte("\"{{ images.[containers, initContainers, ephemeralContainers][].*.reference[] }}\""),
			},
		},
	}

	call, err = New(context.TODO(), entry, ctx, nil, logr.Discard())
	assert.NilError(t, err)
	data, err = call.Execute()
	assert.NilError(t, err)

	expectedResults := `{"images":["https://ghcr.io/tomcat/tomcat:9","https://ghcr.io/vault/vault:v3","https://ghcr.io/busybox/busybox:latest"]}`
	assert.Equal(t, string(expectedResults)+"\n", string(data))
}
