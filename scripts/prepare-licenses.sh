#!/bin/bash
# Copyright 2022 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


set -euo pipefail

tmp=$(mktemp -d)

# This regex matches license files found by https://github.com/google/go-licenses.
licenseRegex="\(LICEN\(S\|C\)E\|COPYING\|README\|NOTICE\)"

# Find all license files in vendor/
# Note that packages may contain multiple license, or may have both a LICENSE
# and a NOTICE which must BOTH be distributed with the code.
licenses="${tmp}"/LICENSES.txt

# Ensure the vendor directory only includes the project's build dependencies.
go mod tidy -compat=1.17
go mod vendor
find vendor/ -regex ".*/${licenseRegex}" > "${licenses}"
sort "${licenses}" -dufo "${licenses}"

# Default to LICENSES.txt as the output file, but allow overriding it.
out="LICENSES.txt"
if [[ $# -gt 0 ]]; then
  out=$1
fi

# Preamble at beginning of LICENSE.
echo "THE FOLLOWING SETS FORTH ATTRIBUTION NOTICES FOR THIRD PARTY SOFTWARE THAT MAY BE CONTAINED IN PORTIONS OF THE ANTHOS CONFIG MANAGEMENT PRODUCT.

-----
" > "${out}"

# For each found license/notice, paste it into LICENSE.
while IFS='' read -r LINE || [[ -n "${LINE}" ]]; do
  package="$(echo "${LINE}" | sed -e "s/^vendor\///" -e "s/\/$licenseRegex$//")"
  {
    echo "The following software may be included in this product: $package. This software contains the following license and notice below:
"
    cat "${LINE}"
    echo "
-----
"
  } >> "${out}"
done < "${licenses}"

rm -r "${tmp}"
