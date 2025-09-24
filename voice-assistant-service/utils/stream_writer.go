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
    
    if _, err := w.Write(data); err != nil {
        return err
    }
    if _, err := w.Write([]byte("\n")); err != nil {
        return err
    }
    
    if flusher, ok := w.(http.Flusher); ok {
        flusher.Flush()
    }
    
    return nil
}