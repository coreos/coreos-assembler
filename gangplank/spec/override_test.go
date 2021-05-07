package spec

import (
	"fmt"
	"testing"
)

func TestOverridePathing(t *testing.T) {
	trueVar := true

	testCases := []struct {
		o       *Override
		desc    string
		want    string
		wantErr bool
	}{
		{
			o: &Override{
				URI: "incorrect://whocares",
			},
			desc:    "invalid no type",
			wantErr: true,
		},
		{
			o: &Override{
				URI: "file://someplace.rpm",
				Rpm: &trueVar,
			},
			want:    "test/overrides/rpm/someplace.rpm",
			desc:    "file rpm",
			wantErr: false,
		},
		{
			o: &Override{
				URI: "https://someplace.rpm",
				Rpm: &trueVar,
			},
			want:    "test/overrides/rpm/someplace.rpm",
			desc:    "net-based rpm",
			wantErr: false,
		},
		{
			o: &Override{
				URI:         "http://tarball-of-doom.tar",
				Tarball:     &trueVar,
				TarballType: strPtr("all"),
			},
			want:    "test/overrides",
			desc:    "tarball of all",
			wantErr: false,
		},
		{
			o: &Override{
				URI:         "http://cuddly-demogorgrans.tar",
				Tarball:     &trueVar,
				TarballType: strPtr("rootfs"),
			},
			want:    "test/overrides/rootfs",
			desc:    "tarball of rootfs",
			wantErr: false,
		},
		{
			o: &Override{
				URI:         "http://tormented-rpms.tar",
				Tarball:     &trueVar,
				TarballType: strPtr("rpms"),
			},
			want:    "test/overrides/rpm",
			desc:    "tarball of rpms",
			wantErr: false,
		},
	}

	for idx, tc := range testCases {
		t.Run(fmt.Sprintf("%d-%s", idx, tc.desc), func(t *testing.T) {
			got, err := tc.o.writePath("test")
			if err != nil && !tc.wantErr {
				t.Fatalf("%s errored unexpectedly", tc.desc)
			}
			if err == nil && tc.wantErr {
				t.Fatalf("%s exepcted error", tc.desc)
			}
			if tc.want != got {
				t.Errorf("%s failed:\n  want: %s\n   got: %s\n", tc.desc, tc.want, got)
			}
		})
	}
}
