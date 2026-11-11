package rust

import "testing"

func TestAPathInsideAnInlineModuleReadsFromTheFileModule(t *testing.T) {
	tests := []struct {
		inline []string
		path   string
		want   string
	}{
		{inline: nil, path: "super::Timestamp", want: "super::Timestamp"},
		{inline: []string{"tests"}, path: "super::Timestamp", want: "self::Timestamp"},
		{inline: []string{"tests"}, path: "self::helper", want: "self::tests::helper"},
		{inline: []string{"tests"}, path: "super::super::Store", want: "super::Store"},
		{inline: []string{"a", "b"}, path: "super::Item", want: "self::a::Item"},
		{inline: []string{"a"}, path: "super::super::super::Top", want: "super::super::Top"},
		{inline: []string{"tests"}, path: "crate::model::User", want: "crate::model::User"},
		{inline: []string{"tests"}, path: "serde::Serialize", want: "serde::Serialize"},
	}
	for _, test := range tests {
		if got := fromFileModule(test.inline, test.path); got != test.want {
			t.Errorf("fromFileModule(%v, %q) = %q, want %q", test.inline, test.path, got, test.want)
		}
	}
}
