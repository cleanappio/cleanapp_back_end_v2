package utils

import (
	"encoding/json"
	"net/http"
	"voice-assistant-service/models"
)

func WriteStreamChunk(w http.ResponseWriter, chunk models.StreamChunk) error {
    data, err := json.Marshal(chunk)
    if err != nil {
        return err
    }
    
    w.Write(data)
    w.Write([]byte("\n"))
    
    if flusher, ok := w.(http.Flusher); ok {
        flusher.Flush()
    }
    
    return nil
}