package main

import (
	"reflect"
	"testing"
)

func TestDocumentPassageBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		input string
		width int
		want  []string
	}{
		{"subscriber notice stays intact", "subscriber access expires Friday", 5, []string{"subscriber access expires Friday"}},
		{"delivery instructions split deterministically", "download asset then confirm receipt to creator", 4, []string{"download asset then confirm", "receipt to creator"}},
		{"blank scan has no passages", "  ", 4, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chunks(tt.input, tt.width); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("chunks = %v, want %v", got, tt.want)
			}
		})
	}
}
