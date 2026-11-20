/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package nvcaps_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcaps"
)

const (
	migIndex = "proc/driver/nvidia-caps/mig-minors"
	driver   = "proc/driver/nvidia/"
	giPath   = "capabilities/gpu3/mig/gi7/access"
	ciPath   = "capabilities/gpu3/mig/gi7/ci2/access"
)

func writeFixture(t *testing.T, root, path, contents string) {
	t.Helper()
	path = filepath.Join(root, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0644))
}

func TestLookupMIGAccessCapabilities(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, migIndex, "config 1\nmonitor 2\ngpu3/gi7/access 66\ngpu3/gi7/ci2/access 69\ngpu3/gi9/access 84\n")
	writeFixture(t, root, driver+giPath, "DeviceFileMinor: 66\nDeviceFileMode: 292\nDeviceFileModify: 1\n")
	writeFixture(t, root, driver+ciPath, "DeviceFileMode: 256\nDeviceFileMinor: 69\n")
	gi := nvcaps.MIGAccessCapability{Path: giPath, Minor: 66, Mode: 0444}
	ci := nvcaps.MIGAccessCapability{Path: ciPath, Minor: 69, Mode: 0400}

	testCases := map[string]struct {
		minors []uint32
		want   []nvcaps.MIGAccessCapability
	}{
		"GI by minor":  {minors: []uint32{66}, want: []nvcaps.MIGAccessCapability{gi}},
		"CI by minor":  {minors: []uint32{69}, want: []nvcaps.MIGAccessCapability{ci}},
		"mixed grants": {minors: []uint32{1, 69, 999, 66, 2, 69}, want: []nvcaps.MIGAccessCapability{ci, gi}},
		"no grants":    {},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got, err := nvcaps.LookupMIGAccessCapabilities(root, tc.minors)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestLookupMIGAccessCapabilitiesErrors(t *testing.T) {
	testCases := map[string]struct {
		record string
		index  string
		want   string
	}{
		"missing record":        {want: "no such file"},
		"missing mode":          {record: "DeviceFileMinor: 66\n", want: "DeviceFileMode"},
		"invalid minor":         {record: "DeviceFileMinor: -1\nDeviceFileMode: 292\n", want: "invalid DeviceFileMinor"},
		"invalid mode":          {record: "DeviceFileMinor: 66\nDeviceFileMode: 512\n", want: "invalid DeviceFileMode"},
		"duplicate mode":        {record: "DeviceFileMinor: 66\nDeviceFileMode: 292\nDeviceFileMode: 256\n", want: "duplicate DeviceFileMode"},
		"stale index":           {record: "DeviceFileMinor: 67\nDeviceFileMode: 292\n", want: "advertises minor 67"},
		"ambiguous index":       {index: "gpu3/gi7/ci2/access 66\n", want: "maps minor 66 to both"},
		"unreadable index line": {index: strings.Repeat("x", 70*1024), want: "reading MIG minors file"},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, root, migIndex, "gpu3/gi7/access 66\n"+tc.index)
			if tc.record != "" {
				writeFixture(t, root, driver+giPath, tc.record)
			}
			got, err := nvcaps.LookupMIGAccessCapabilities(root, []uint32{66})
			require.Nil(t, got)
			require.ErrorContains(t, err, tc.want)
			if name == "missing record" {
				require.ErrorIs(t, err, fs.ErrNotExist)
			}
		})
	}
}

func TestLookupMIGAccessCapabilitiesWithoutIndex(t *testing.T) {
	got, err := nvcaps.LookupMIGAccessCapabilities(t.TempDir(), []uint32{66})
	require.NoError(t, err)
	require.Empty(t, got)
}
