package api

import (
	"context"
	"database/sql"
	"net/url"
	"strconv"
	"strings"

	sebufhttp "github.com/SebastienMelki/sebuf/http"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/notify"
	"github.com/hasanMshawrab/idios/internal/query"
	"github.com/hasanMshawrab/idios/internal/store"
)

// AddCluster inserts a cluster row for a kubeconfig context and answers the
// row as it stands after the write.
func (s *Server) AddCluster(ctx context.Context, req *idiosv1.AddClusterRequest) (*idiosv1.Cluster, error) {
	if s.writer == nil {
		return nil, errNoWriter
	}
	if err := validateAddCluster(req); err != nil {
		return nil, err
	}
	at := clock.Format(s.clk.Now())
	// dup is set from inside the Tx and read after it returns: Writer.Tx joins
	// a returned error with the rollback outcome, which would strip the
	// ValidationError of the proto.Message type the generated handler matches
	// on, so the closure always returns nil and the caller decides instead.
	var id int64
	var dup *store.Cluster
	if err := s.writer.Tx(ctx, func(tx *sql.Tx) error {
		existing, err := store.FindClusterByName(ctx, tx, req.GetName())
		if err != nil {
			return err
		}
		if existing != nil {
			dup = existing
			return nil
		}
		id, err = store.InsertCluster(ctx, tx, req.GetName(), req.GetContextName(), at)
		return err
	}); err != nil {
		return nil, err
	}
	if dup != nil {
		return nil, &sebufhttp.ValidationError{Violations: []*sebufhttp.FieldViolation{{
			Field: "name",
			Description: `cluster "` + req.GetName() + `" exists (id ` +
				strconv.FormatInt(dup.ID, 10) + `)`,
		}}}
	}
	s.events.Notify(notify.Cluster, id)
	return s.clusterRowAfter(ctx, id)
}

// RenameCluster sets the friendly name and answers the row as it stands
// after the write.
func (s *Server) RenameCluster(ctx context.Context, req *idiosv1.RenameClusterRequest) (*idiosv1.Cluster, error) {
	if s.writer == nil {
		return nil, errNoWriter
	}
	if err := validateName(req.GetName()); err != nil {
		return nil, err
	}
	id := req.GetId()
	found := false
	if err := s.writer.Tx(ctx, func(tx *sql.Tx) error {
		existing, err := store.GetCluster(ctx, tx, id)
		if err != nil || existing == nil {
			return err
		}
		found = true
		_, err = store.RenameCluster(ctx, tx, id, req.GetName())
		return err
	}); err != nil {
		return nil, err
	}
	if !found {
		return nil, &notFoundError{what: "cluster", id: strconv.FormatInt(id, 10)}
	}
	s.events.Notify(notify.Cluster, id)
	return s.clusterRowAfter(ctx, id)
}

// SetClusterGrafana sets or clears the cluster's Grafana configuration and
// answers the row as it stands after the write.
func (s *Server) SetClusterGrafana(ctx context.Context, req *idiosv1.SetClusterGrafanaRequest) (*idiosv1.Cluster, error) {
	if s.writer == nil {
		return nil, errNoWriter
	}
	if err := validateSetClusterGrafana(req); err != nil {
		return nil, err
	}
	id := req.GetId()
	found := false
	if err := s.writer.Tx(ctx, func(tx *sql.Tx) error {
		existing, err := store.GetCluster(ctx, tx, id)
		if err != nil || existing == nil {
			return err
		}
		found = true
		_, err = store.SetClusterGrafana(ctx, tx, id, req.GetGrafanaUrl(), req.GetLokiDatasourceUid(), req.GetLogSelector())
		return err
	}); err != nil {
		return nil, err
	}
	if !found {
		return nil, &notFoundError{what: "cluster", id: strconv.FormatInt(id, 10)}
	}
	s.events.Notify(notify.Cluster, id)
	return s.clusterRowAfter(ctx, id)
}

// DeleteCluster removes the cluster's rows and artifact directory. It
// answers the empty response whether or not the cluster existed.
func (s *Server) DeleteCluster(ctx context.Context, req *idiosv1.DeleteClusterRequest) (*idiosv1.DeleteClusterResponse, error) {
	if s.writer == nil {
		return nil, errNoWriter
	}
	id := req.GetId()
	if err := s.writer.Tx(ctx, func(tx *sql.Tx) error {
		_, err := store.RemoveCluster(ctx, tx, id)
		return err
	}); err != nil {
		return nil, err
	}
	// The commit is what removed the cluster; the subscribers learn of it
	// whatever happens to the directory next.
	s.events.Notify(notify.Cluster, id)
	// After the commit: a directory left by a crash between the two is the
	// orphan-files sweep's to clean up, not this request's to wait for.
	if err := removeClusterDir(s.cfg.ArtifactsRoot, id); err != nil {
		return nil, err
	}
	return &idiosv1.DeleteClusterResponse{}, nil
}

// AddWatchedNamespace starts watching a namespace in the cluster and answers
// the row as it stands after the write.
func (s *Server) AddWatchedNamespace(ctx context.Context, req *idiosv1.AddWatchedNamespaceRequest) (*idiosv1.Cluster, error) {
	if s.writer == nil {
		return nil, errNoWriter
	}
	if err := validateName(req.GetName()); err != nil {
		return nil, err
	}
	id := req.GetId()
	at := clock.Format(s.clk.Now())
	found := false
	if err := s.writer.Tx(ctx, func(tx *sql.Tx) error {
		existing, err := store.GetCluster(ctx, tx, id)
		if err != nil || existing == nil {
			return err
		}
		found = true
		_, err = store.AddWatchedNamespace(ctx, tx, id, req.GetName(), at)
		return err
	}); err != nil {
		return nil, err
	}
	if !found {
		return nil, &notFoundError{what: "cluster", id: strconv.FormatInt(id, 10)}
	}
	s.events.Notify(notify.Cluster, id)
	return s.clusterRowAfter(ctx, id)
}

// RemoveWatchedNamespace stops watching a namespace in the cluster and
// answers the row as it stands after the write.
func (s *Server) RemoveWatchedNamespace(ctx context.Context, req *idiosv1.RemoveWatchedNamespaceRequest) (*idiosv1.Cluster, error) {
	if s.writer == nil {
		return nil, errNoWriter
	}
	id := req.GetId()
	found := false
	if err := s.writer.Tx(ctx, func(tx *sql.Tx) error {
		existing, err := store.GetCluster(ctx, tx, id)
		if err != nil || existing == nil {
			return err
		}
		found = true
		_, err = store.RemoveWatchedNamespace(ctx, tx, id, req.GetName())
		return err
	}); err != nil {
		return nil, err
	}
	if !found {
		return nil, &notFoundError{what: "cluster", id: strconv.FormatInt(id, 10)}
	}
	s.events.Notify(notify.Cluster, id)
	return s.clusterRowAfter(ctx, id)
}

// clusterRowAfter reloads the merged row the scope list would show for id,
// the same way ListClusters builds it.
func (s *Server) clusterRowAfter(ctx context.Context, id int64) (*idiosv1.Cluster, error) {
	rows, err := query.ListClusters(ctx, s.db)
	if err != nil {
		return nil, err
	}
	live := s.clusterState()
	for _, r := range rows {
		if r.ID == id {
			return cluster(r, live[id]), nil
		}
	}
	return nil, &notFoundError{what: "cluster", id: strconv.FormatInt(id, 10)}
}

// validateAddCluster reports one violation per empty required field.
func validateAddCluster(req *idiosv1.AddClusterRequest) error {
	var violations []*sebufhttp.FieldViolation
	if req.GetContextName() == "" {
		violations = append(violations, &sebufhttp.FieldViolation{
			Field: "context_name", Description: "context_name is required",
		})
	}
	if req.GetName() == "" {
		violations = append(violations, &sebufhttp.FieldViolation{
			Field: "name", Description: "name is required",
		})
	}
	if len(violations) == 0 {
		return nil
	}
	return &sebufhttp.ValidationError{Violations: violations}
}

// validateSetClusterGrafana reports one violation per invalid field; an
// empty grafana_url is the clear and skips the other checks, since clearing
// the feature does not need a datasource or a selector.
func validateSetClusterGrafana(req *idiosv1.SetClusterGrafanaRequest) error {
	if req.GetGrafanaUrl() == "" {
		return nil
	}
	var violations []*sebufhttp.FieldViolation
	if !isAbsoluteHTTPURL(req.GetGrafanaUrl()) {
		violations = append(violations, &sebufhttp.FieldViolation{
			Field: "grafana_url", Description: "grafana_url must be an absolute http or https URL",
		})
	}
	if req.GetLokiDatasourceUid() == "" {
		violations = append(violations, &sebufhttp.FieldViolation{
			Field: "loki_datasource_uid", Description: "loki_datasource_uid is required",
		})
	}
	if req.GetLogSelector() == "" || !strings.Contains(req.GetLogSelector(), "$pod") {
		violations = append(violations, &sebufhttp.FieldViolation{
			Field: "log_selector", Description: "log_selector is required and must contain $pod",
		})
	}
	if len(violations) == 0 {
		return nil
	}
	return &sebufhttp.ValidationError{Violations: violations}
}

// isAbsoluteHTTPURL reports whether s parses as an absolute http or https
// URL with a host.
func isAbsoluteHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// validateName reports a violation when the required friendly or namespace
// name is empty.
func validateName(name string) error {
	if name != "" {
		return nil
	}
	return &sebufhttp.ValidationError{Violations: []*sebufhttp.FieldViolation{
		{Field: "name", Description: "name is required"},
	}}
}
