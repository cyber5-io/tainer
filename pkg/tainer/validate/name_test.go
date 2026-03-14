package validate

import (
	"strings"
	"testing"
)

func TestProjectName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple", "my-client", false},
		{"valid single char", "a", false},
		{"valid with numbers", "site2", false},
		{"valid max length 63", strings.Repeat("a", 63), false},
		{"empty", "", true},
		{"starts with hyphen", "-foo", true},
		{"ends with hyphen", "foo-", true},
		{"uppercase", "MyClient", true},
		{"spaces", "my client", true},
		{"underscores", "my_client", true},
		{"dots", "my.client", true},
		{"too long 64", strings.Repeat("a", 64), true},
		{"special chars", "my@client", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ProjectName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ProjectName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
