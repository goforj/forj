package stacks

import (
	"strings"
	"testing"
)

// TestDocumentReplacementPreservesUnrelatedMultilineAndCRLF keeps syntax outside the managed boundary byte-stable.
func TestDocumentReplacementPreservesUnrelatedMultilineAndCRLF(t *testing.T) {
	source := "# owner\r\nAPP_KEY='line one\r\nline two'\r\nexport DB_PASSWORD='old\r\npassword'\r\nDB_PASSWORD=last\r\nAPP_NAME=keep\r\n"
	d, err := parseDocument([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	output := d.replace(func(key string) bool { return strings.HasPrefix(key, "DB_") }, map[string]string{"DB_PASSWORD": "literal $TOKEN\nsecond line", "DB_DRIVER": "sqlite"})
	next, err := parseDocument(output)
	if err != nil {
		t.Fatal(err)
	}
	if next.values["DB_PASSWORD"] != "literal $TOKEN\nsecond line" {
		t.Fatal("value did not round trip")
	}
	if strings.Count(string(output), "DB_PASSWORD=") != 1 {
		t.Fatal("duplicate assignment retained")
	}
	if !strings.Contains(string(output), "APP_KEY='line one\r\nline two'\r\n") {
		t.Fatal("unrelated multiline changed")
	}
}

// TestDocumentRejectsMalformedInput checks errors before any configuration can be replaced.
func TestDocumentRejectsMalformedInput(t *testing.T) {
	for _, source := range []string{"DB_PASSWORD='unfinished", "1KEY=value", "DB.BAD=value", "not an assignment"} {
		if _, err := parseDocument([]byte(source)); err == nil {
			t.Errorf("accepted %q", source)
		}
	}
}
