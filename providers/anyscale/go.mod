module github.com/xraph/nexus/providers/anyscale

go 1.25

require (
	github.com/xraph/nexus v0.0.0
	github.com/xraph/nexus/providers/openai v0.0.0
)

replace (
	github.com/xraph/nexus => ../..
	github.com/xraph/nexus/providers/openai => ../openai
)
