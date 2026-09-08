package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	sebufhttp "github.com/SebastienMelki/sebuf/http"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
)

// requestTimeout bounds one call: an agent waiting on a wedged daemon has
// no way to interrupt the tool it asked for.
const requestTimeout = 30 * time.Second

// contentLimit is the largest artifact body read into memory. Trimming
// happens here rather than in the daemon, so the whole file crosses the
// loopback interface and a runaway file must still not exhaust the process.
const contentLimit = 64 << 20

// daemon is the only view this server has of idios: the generated client of
// the HTTP API, plus the artifact content endpoint, which serves raw bytes
// and so is outside the proto contract.
type daemon struct {
	addr string
	api  idiosv1.IdiosServiceClient
	http *http.Client
}

// newDaemon builds a client of the daemon listening on addr.
func newDaemon(addr string) *daemon {
	c := &http.Client{Timeout: requestTimeout}
	return &daemon{addr: addr, api: idiosv1.NewIdiosServiceClient("http://"+addr, idiosv1.WithIdiosServiceHTTPClient(c)), http: c}
}

// unreachable is the one answer every tool gives when nothing is listening:
// a raw dial failure names a syscall and a port, not what to do about it.
func (d *daemon) unreachable(err error) error {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return fmt.Errorf("no idios daemon answered at %s: start one with 'idios run', or point this server elsewhere with 'idios mcp -daemon host:port'", d.addr)
	}
	return err
}

// content reads the captured bytes of one artifact. A daemon that has no
// file for the row answers 404 with the reason, which is the answer.
func (d *daemon) content(ctx context.Context, id int64) ([]byte, error) {
	url := "http://" + d.addr + "/v1/artifacts/" + strconv.FormatInt(id, 10) + "/content"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, d.unreachable(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, contentLimit))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, contentError(resp.Status, body)
	}
	return body, nil
}

// contentError unwraps the daemon's error body, which carries the capture
// gap and note when the row has no file.
func contentError(status string, body []byte) error {
	var e sebufhttp.Error
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, &e); err == nil && e.GetMessage() != "" {
		return errors.New(e.GetMessage())
	}
	return errors.New("artifact content: " + status)
}

// encode renders a response as the JSON the daemon itself sends: a message
// whose enums have a custom wire spelling marshals itself. protojson varies
// its whitespace on purpose, so the bytes are compacted to stay stable.
func encode(msg proto.Message) (json.RawMessage, error) {
	var (
		data []byte
		err  error
	)
	if m, ok := any(msg).(json.Marshaler); ok {
		data, err = m.MarshalJSON()
	} else {
		data, err = protojson.Marshal(msg)
	}
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Compact(&out, data); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// clusterID resolves a cluster name to the id the API filters on. Tools
// take the name because that is what a human and the application show.
func (d *daemon) clusterID(ctx context.Context, name string) (int64, error) {
	list, err := d.api.ListClusters(ctx, &idiosv1.ListClustersRequest{})
	if err != nil {
		return 0, d.unreachable(err)
	}
	known := make([]string, 0, len(list.GetClusters()))
	for _, c := range list.GetClusters() {
		if c.GetName() == name {
			return c.GetId(), nil
		}
		known = append(known, c.GetName())
	}
	if len(known) == 0 {
		return 0, fmt.Errorf("no cluster named %q: this daemon watches no clusters", name)
	}
	return 0, fmt.Errorf("no cluster named %q: this daemon watches %v", name, known)
}
