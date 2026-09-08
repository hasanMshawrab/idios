package api

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/hasanMshawrab/idios/internal/store"
)

// removeArtifactFiles deletes the files of the given rows under root and
// stops at the first failure, so the rows stay and a retry finds them. A
// file already gone is not a failure: the row is what says it existed.
func removeArtifactFiles(root string, files []store.ArtifactFile) error {
	for _, f := range files {
		path, ok := underRoot(root, f.FilePath)
		if !ok {
			return errors.New("artifact " + f.FilePath + " path escapes the artifacts root")
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// removeClusterDir deletes the cluster's artifact directory and everything
// under it. Removing an absent directory is not an error.
func removeClusterDir(root string, id int64) error {
	return os.RemoveAll(filepath.Join(root, strconv.FormatInt(id, 10)))
}
