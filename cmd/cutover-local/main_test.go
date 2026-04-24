package main

import "testing"

func TestEffectivePreserveDBName(t *testing.T) {
	tests := []struct {
		name       string
		targetDB   string
		preserveDB string
		want       string
	}{
		{
			name:       "uses explicit preserve db",
			targetDB:   "controlhub",
			preserveDB: "controlhub_v1",
			want:       "controlhub_v1",
		},
		{
			name:       "defaults preserve db from target",
			targetDB:   "sandbox",
			preserveDB: "",
			want:       "sandbox_v1",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := effectivePreserveDBName(testCase.targetDB, testCase.preserveDB)
			if got != testCase.want {
				t.Fatalf("effectivePreserveDBName() = %q, want %q", got, testCase.want)
			}
		})
	}
}
