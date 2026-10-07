module github.com/xraph/nexus/providers/lepton

go 1.26.0

require (
	github.com/xraph/nexus v1.6.2
	github.com/xraph/nexus/providers/openai v1.6.2
)

require github.com/shopspring/decimal v1.4.0 // indirect

replace (
	github.com/xraph/nexus => ../..
	github.com/xraph/nexus/providers/openai => ../openai
)
