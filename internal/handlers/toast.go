package handlers

import (
	"encoding/json"
	"net/http"
)

func triggerToast(w http.ResponseWriter, variant, message string) {
	payload, _ := json.Marshal(map[string]any{
		"toast": map[string]string{"variant": variant, "message": message},
	})
	w.Header().Set("HX-Trigger", string(payload))
}
