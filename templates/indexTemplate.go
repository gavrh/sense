package templates

type IndexTemplate struct {
	AccountId string
	UserLogin string
	MainDisplay MainTemplate
}

func NewIndexTemplate(mainDisplay MainTemplate) IndexTemplate {
	return IndexTemplate {
		MainDisplay: mainDisplay,
	}
}
