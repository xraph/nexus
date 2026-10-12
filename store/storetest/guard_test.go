package storetest

import "testing"

func TestCheckPostgresDSN(t *testing.T) {
	t.Setenv("PGPORT", "")
	for dsn, refused := range map[string]bool{
		"postgres://postgres:nexus@localhost/nexus?sslmode=disable":       true,
		"postgres://postgres:nexus@localhost:5432/nexus?sslmode=disable":  true,
		"host=localhost user=postgres dbname=nexus":                       true,
		"host=localhost port=5432 user=postgres dbname=nexus":             true,
		"postgres://u@a:55632,b/nexus":                                    true,
		"postgres://postgres:nexus@localhost:55632/nexus?sslmode=disable": false,
		"postgres://postgres:nexus@localhost:54320/nexus?sslmode=disable": false,
		"host=localhost port=55632 user=postgres dbname=nexus":            false,
		"postgres://postgres:nexus@localhost:15432/nexus?sslmode=disable": false,
	} {
		err := checkPostgresDSN(dsn)
		if (err != nil) != refused {
			t.Errorf("checkPostgresDSN(%q) = %v, refused want %v", dsn, err, refused)
		}
	}
	if err := checkPostgresDSN("postgres://localhost:notaport/x"); err == nil {
		t.Error("an unparseable DSN must be refused")
	}
}

func TestCheckMongoURI(t *testing.T) {
	for uri, refused := range map[string]bool{
		"mongodb://localhost/nexus_test":               true,
		"mongodb://localhost":                          true,
		"mongodb://localhost:27017/nexus_test":         true,
		"mongodb://u:p@localhost/nexus_test?x=y:57632": true,
		"mongodb://[::1]/nexus_test":                   true,
		"mongodb://a:57632,b/nexus_test":               true,
		"mongodb+srv://cluster.example.net/nexus_test": true,
		"localhost:57632":                              true,
		"mongodb://localhost:57632/nexus_test":         false,
		"mongodb://localhost:270170/nexus_test":        false,
		"mongodb://u:p@localhost:57632/nexus_test":     false,
		"mongodb://[::1]:57632/nexus_test":             false,
		"mongodb://a:57632,b:57633/nexus_test":         false,
	} {
		err := checkMongoURI(uri)
		if (err != nil) != refused {
			t.Errorf("checkMongoURI(%q) = %v, refused want %v", uri, err, refused)
		}
	}
}
