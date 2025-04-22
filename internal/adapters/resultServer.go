package adapters

type ResultServerAdapter struct {
	BaseUrl string
}

func NewResultServerAdapter(baseUrl string) *ResultServerAdapter {
	return &ResultServerAdapter{BaseUrl: baseUrl}
}
