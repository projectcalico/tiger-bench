// Copyright (c) 2024-2025 Tigera, Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"bytes"
	"os/exec"

	log "github.com/sirupsen/logrus"
)

// Shellout is a function that runs a command in a shell and returns the stdout and stderr
func Shellout(command string, retries int) (string, string, error) {
	const shellToUse = "sh"
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var err error
	for i := 0; i < retries; i++ {
		log.Debug("Running command: ", command)
		stdout.Reset()
		stderr.Reset()
		cmd := exec.Command(shellToUse, "-c", command)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err = cmd.Run()
		if err != nil {
			log.WithError(err).Error("Error running command ", command)
			log.Error("stdout: ", stdout.String())
			log.Error("stderr: ", stderr.String())
		} else {
			return stdout.String(), stderr.String(), nil
		}
	}
	return stdout.String(), stderr.String(), err
}
