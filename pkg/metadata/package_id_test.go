package metadata

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/validation"
	"kpt.dev/configsync/pkg/api/configsync"
)

const (
	// maxLabelLength = validation.LabelValueMaxLength // 63
	maxNameLength = validation.DNS1123SubdomainMaxLength // 253
)
const loremIpsum = "Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum."

func TestPackageIDLabelValue(t *testing.T) {
	loremIpsumValid := strings.ReplaceAll(loremIpsum, " ", "")
	loremIpsumValid = strings.ReplaceAll(loremIpsumValid, ",", "")
	loremIpsumValid = strings.ReplaceAll(loremIpsumValid, ".", "")
	loremIpsumValid = strings.ToLower(loremIpsumValid)
	loremIpsumReverse := reverse(loremIpsumValid)

	type args struct {
		syncName      string
		syncNamespace string
		syncKind      string
	}
	tests := []struct {
		name string
		in   args
		out  string
	}{
		{
			// As long as possible
			name: "253 char name & 63 char namespace",
			in: args{
				syncName:      loremIpsumValid[0 : maxNameLength-1],
				syncNamespace: loremIpsumReverse[0 : maxLabelLength-1],
				syncKind:      configsync.RootSyncKind,
			},
			out: "loremipsumdolorsitametconsecteturadipiscingelitseddoe-9758dfab",
		},
		{
			// Padded checksum
			name: "63 char name & 63 char namespace",
			in: args{
				syncName:      loremIpsumValid[0 : maxLabelLength-1],
				syncNamespace: loremIpsumReverse[0 : maxLabelLength-1],
				syncKind:      configsync.RootSyncKind,
			},
			out: "loremipsumdolorsitametconsecteturadipiscingelitseddoe-0e8e8264",
		},
		{
			// Trailing dot
			name: "53 char name & 63 char namespace",
			in: args{
				syncName:      loremIpsumValid[0:52],
				syncNamespace: loremIpsumReverse[0 : maxLabelLength-1],
				syncKind:      configsync.RootSyncKind,
			},
			out: "loremipsumdolorsitametconsecteturadipiscingelitseddo.-ea053540",
		},
		{
			// Just a little too long
			name: "30 char name & 30 char namespace",
			in: args{
				syncName:      loremIpsumValid[0:29],
				syncNamespace: loremIpsumReverse[0:29],
				syncKind:      configsync.RepoSyncKind,
			},
			out: "loremipsumdolorsitametconsect.murobaltsediminatillomt-ff884db0",
		},
		{
			// Short enough to not hash
			name: "10 char name & 10 char namespace",
			in: args{
				syncName:      loremIpsumValid[0:9],
				syncNamespace: loremIpsumReverse[0:9],
				syncKind:      configsync.RepoSyncKind,
			},
			out: "loremipsu.murobalts.RepoSync",
		},
		{
			// As short as possible
			name: "1 char name & 1 char namespace",
			in: args{
				syncName:      loremIpsumValid[0:1],
				syncNamespace: loremIpsumReverse[0:1],
				syncKind:      configsync.RepoSyncKind,
			},
			out: "l.m.RepoSync",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := PackageID(tt.in.syncName, tt.in.syncNamespace, tt.in.syncKind)
			assert.Equal(t, tt.out, out)
			assert.LessOrEqual(t, len(out), 63)
		})
	}
}

func reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}
