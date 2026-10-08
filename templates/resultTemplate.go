package templates

type ResultTemplate struct {
	SourceTitle string
	SourceURL   string
	Answer      string
	Confidence  float64
}

func NewResultTemplate(sourceTitle, sourceURL, answer string, confidence float64) ResultTemplate {
	return ResultTemplate{
		SourceTitle: sourceTitle,
		SourceURL:   sourceURL,
		Answer:      answer,
		Confidence:  confidence,
	}
}

type MessageTemplate struct {
	Message string
}

func NewMessageTemplate(message string) MessageTemplate {
	return MessageTemplate{
		Message: message,
	}
}
