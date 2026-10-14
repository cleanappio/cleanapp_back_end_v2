package evidence

// Classifier uses the same narrow source-only schema across provider clients.
type Classifier interface {
	AnalyzeEvidence(original, entity string) (Fields, error)
}

// ImageClassifier accepts retained original media rather than generated analysis images.
type ImageClassifier interface {
	AnalyzeEvidenceImage(image []byte, original, entity string) (Fields, error)
}

// OriginalClassifier retains source-author/repost context beside original text
// and media. Acquisition actor_kind is not the public source author's identity.
type OriginalClassifier interface {
	AnalyzeOriginalEvidence(original Original, entity string) (Fields, error)
}

func Classify(client Classifier, original Original, entity string) (Fields, error) {
	if original.Text == "" && !ValidOriginalImage(original.Image) {
		return Unknown(original.Reason), nil
	}
	if client == nil {
		return Unknown("Evidence classifier is unavailable."), nil
	}
	var fields Fields
	var err error
	if sourceClient, ok := client.(OriginalClassifier); ok {
		fields, err = sourceClient.AnalyzeOriginalEvidence(original, entity)
	} else if mediaClient, ok := client.(ImageClassifier); ok && ValidOriginalImage(original.Image) {
		fields, err = mediaClient.AnalyzeEvidenceImage(original.Image, original.Text, entity)
	} else {
		fields, err = client.AnalyzeEvidence(original.Text, entity)
	}
	if err != nil {
		return Unknown("Evidence classification failed; retained source remains available."), err
	}
	_, supportsImage := client.(ImageClassifier)
	if _, ok := client.(OriginalClassifier); ok {
		supportsImage = true
	}
	return fields.NormalizeWithMedia(original.Text, supportsImage && ValidOriginalImage(original.Image)), nil
}

// ForPersistence leaves transient failures retryable. A successful unknown
// classification (insufficient evidence) is a completed versioned enrichment.
func ForPersistence(client Classifier, original Original, entity string, resolutionErr error) (Fields, int, error) {
	if resolutionErr != nil {
		return Unknown("Stored original could not be resolved."), 0, resolutionErr
	}
	fields, err := Classify(client, original, entity)
	if err != nil {
		return fields, 0, err
	}
	return fields, Version, nil
}
