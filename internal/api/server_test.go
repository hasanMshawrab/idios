package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	sebufhttp "github.com/SebastienMelki/sebuf/http"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/config"
	"github.com/hasanMshawrab/idios/internal/query/querytest"
)

// A status this package sets carries its own content type, so it has to
// negotiate exactly as the generated writer that marshals the body does:
// a header and a body that disagree are unreadable to every client.
func TestErrorBodyMatchesNegotiatedContentType(t *testing.T) {
	const message = "incident 999999 not found"
	cases := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{
			name:    "protobuf with parameters",
			headers: map[string]string{"Accept": "application/x-protobuf; charset=utf-8"},
			want:    idiosv1.ProtoContentType,
		},
		{
			name:    "octet stream",
			headers: map[string]string{"Accept": idiosv1.BinaryContentType},
			want:    idiosv1.BinaryContentType,
		},
		{
			name:    "a wildcard accept falls back to the request type",
			headers: map[string]string{"Accept": "*/*", "Content-Type": idiosv1.ProtoContentType},
			want:    idiosv1.ProtoContentType,
		},
		{
			name:    "anything else is json",
			headers: map[string]string{"Accept": "text/html"},
			want:    idiosv1.JSONContentType,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fixtureServer{err: &notFoundError{what: "incident", id: "999999"}}
			code, contentType, body := serveFixtureWith(t, f, "/v1/incidents", c.headers)
			if code != http.StatusNotFound {
				t.Fatalf("status %d, want %d: %s", code, http.StatusNotFound, body)
			}
			if contentType != c.want {
				t.Errorf("content type %q, want %q", contentType, c.want)
			}
			var got sebufhttp.Error
			var err error
			if c.want == idiosv1.JSONContentType {
				err = protojson.Unmarshal(body, &got)
			} else {
				err = proto.Unmarshal(body, &got)
			}
			if err != nil {
				t.Fatalf("body is not %s: %v: %q", c.want, err, body)
			}
			if got.GetMessage() != message {
				t.Errorf("message %q, want %q", got.GetMessage(), message)
			}
		})
	}
}

// A failing read answers 500 with a fixed message: the underlying error would
// otherwise put SQL text in front of the user.
func TestReadFailureIs500WithoutSQLText(t *testing.T) {
	st, _ := querytest.Seed(t)
	db := st.Reader.DB()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(db, config.Default(), clock.Real{}, slog.New(slog.DiscardHandler)).Handler())
	defer srv.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/v1/incidents", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d, want %d: %s", resp.StatusCode, http.StatusInternalServerError, body)
	}
	if got, want := string(compact(t, body)), `{"message":"internal error"}`; got != want {
		t.Errorf("body %s, want %s", got, want)
	}
}
