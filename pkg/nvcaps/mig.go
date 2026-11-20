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

// Package nvcaps reads NVIDIA driver capability metadata.
package nvcaps

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/nvcaps"
)

// MIGAccessCapability describes the driver record for a GPU instance (GI) or
// compute instance (CI) access capability.
type MIGAccessCapability struct {
	// Path is the capability record path relative to /proc/driver/nvidia.
	Path string
	// Minor is the nvidia-caps character-device minor from DeviceFileMinor.
	Minor uint32
	// Mode is the device permission mode advertised by DeviceFileMode.
	Mode uint32
}

// LookupMIGAccessCapabilities reads GI and CI access records for the supplied
// nvidia-caps device minors. It returns each match once, in input order. It
// skips minors absent from the driver's MIG index and management capabilities
// such as config and monitor. A matching index entry with a missing,
// malformed, or inconsistent record returns an error.
//
// The root is the filesystem root containing proc; an empty root uses the
// current filesystem's /proc. This function does not initialize NVML or
// determine which device minors the caller is authorized to use.
func LookupMIGAccessCapabilities(root string, minors []uint32) ([]MIGAccessCapability, error) {
	if len(minors) == 0 {
		return nil, nil
	}
	index, err := nvcaps.NewMigCapsFromRoot(root)
	if err != nil {
		return nil, err
	}

	capsByMinor := make(map[uint32]nvcaps.MigCap, len(minors))
	for _, minor := range minors {
		capsByMinor[minor] = ""
	}
	for cap, indexedMinor := range index {
		if cap == "config" || cap == "monitor" || uint64(indexedMinor) > math.MaxUint32 {
			continue
		}
		minor := uint32(indexedMinor)
		previous, requested := capsByMinor[minor]
		if !requested {
			continue
		}
		if previous != "" {
			return nil, fmt.Errorf("MIG capability index maps minor %d to both %q and %q", minor, previous, cap)
		}
		capsByMinor[minor] = cap
	}

	var result []MIGAccessCapability
	for _, minor := range minors {
		cap := capsByMinor[minor]
		if cap == "" {
			continue
		}
		delete(capsByMinor, minor)

		path := filepath.Join(root, cap.ProcPath())
		record, err := readAccessRecord(path)
		if err != nil {
			return nil, fmt.Errorf("reading MIG capability record %q: %w", path, err)
		}
		if record.Minor != minor {
			return nil, fmt.Errorf("MIG capability record %q advertises minor %d, but the index lists minor %d", path, record.Minor, minor)
		}
		record.Path = cap.RelativeProcPath()
		result = append(result, record)
	}
	return result, nil
}

func readAccessRecord(path string) (MIGAccessCapability, error) {
	file, err := os.Open(path)
	if err != nil {
		return MIGAccessCapability{}, err
	}
	defer file.Close()

	var record MIGAccessCapability
	fields := map[string]*uint32{
		"DeviceFileMinor": &record.Minor,
		"DeviceFileMode":  &record.Mode,
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok || (key != "DeviceFileMinor" && key != "DeviceFileMode") {
			continue
		}
		field, exists := fields[key]
		if !exists {
			return MIGAccessCapability{}, fmt.Errorf("duplicate %s", key)
		}
		parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
		if err != nil {
			return MIGAccessCapability{}, fmt.Errorf("invalid %s: %w", key, err)
		}
		*field = uint32(parsed)
		delete(fields, key)
	}
	if err := scanner.Err(); err != nil {
		return MIGAccessCapability{}, err
	}
	if len(fields) != 0 {
		return MIGAccessCapability{}, fmt.Errorf("expected both DeviceFileMinor and DeviceFileMode")
	}
	if record.Mode > 0777 {
		return MIGAccessCapability{}, fmt.Errorf("invalid DeviceFileMode %d", record.Mode)
	}
	return record, nil
}
