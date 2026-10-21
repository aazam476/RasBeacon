package webserver

// Large uploads (updates, plugin files, backups to restore) and the backup
// download can take longer than any fixed time limit would allow: a 6 GB
// backup with plugin data takes over half an hour at a Pi Zero's speed. So
// they have a time limit that moves instead: each time data arrives or goes
// out, the connection may wait transferIdleTime more. A transfer that
// keeps moving can take as long as it needs; one that stalls ends.

import (
	"io"
	"net/http"
	"time"

	"rasbeacon/internal/api"
)

// transferIdleTime is how long a transfer may go without any data. A
// variable so tests can make it short.
var transferIdleTime = 2 * time.Minute

// slidingReader reads an upload, moving the connection's read deadline on
// each time data arrives.
type slidingReader struct {
	r  io.Reader
	rc *http.ResponseController
}

// Read reads from the upload and gives the connection transferIdleTime
// more for the next data.
func (s slidingReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		_ = s.rc.SetReadDeadline(time.Now().Add(transferIdleTime))
	}
	return n, err
}

// slidingWriter writes a download, giving each write transferIdleTime to
// get out.
type slidingWriter struct {
	w  io.Writer
	rc *http.ResponseController
}

// Write moves the write deadline, then writes.
func (s slidingWriter) Write(p []byte) (int, error) {
	_ = s.rc.SetWriteDeadline(time.Now().Add(transferIdleTime))
	return s.w.Write(p)
}

// streamUpload streams a large upload, at most maxSize bytes, to rbd at
// rbdPath and passes rbd's answer back. rbd may take a while once the
// upload is in (writing an update takes minutes), so the answer has no
// write deadline until it is ready.
func (s *Server) streamUpload(w http.ResponseWriter, r *http.Request, rbdPath string, maxSize int64) {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(transferIdleTime))
	_ = rc.SetWriteDeadline(time.Time{}) // none while uploading; set for the answer
	body := slidingReader{r: http.MaxBytesReader(w, r.Body, maxSize), rc: rc}
	status, answer, err := s.rbd.Stream(rbdPath, body)
	_ = rc.SetWriteDeadline(time.Now().Add(transferIdleTime))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: err.Error()})
		return
	}
	writeRaw(w, status, "application/json", answer)
}
