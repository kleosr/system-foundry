package streaming

import (
	"net/http"
	"strings"

	"systemsfoundry/auth"
)

func WriteEvent(w http.ResponseWriter, msg Message) error {
	event := msg.Event
	if event != "token" && event != "change" {
		event = "token"
	}
	data := strings.ReplaceAll(strings.ReplaceAll(msg.Data, "\r", " "), "\n", " ")
	_, err := w.Write([]byte("event: " + event + "\ndata: " + data + "\n\n"))
	return err
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.ActorFrom(r.Context())
	if !ok || !auth.Can(actor, auth.ActionRead, actor.TenantID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	sub := h.Subscribe(actor.TenantID)
	defer sub.Cancel()
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.Closed:
			return
		case msg := <-sub.Messages:
			if err := WriteEvent(w, msg); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
