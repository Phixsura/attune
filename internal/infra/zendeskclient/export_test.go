// SPDX-License-Identifier: Apache-2.0

// export_test.go exposes unexported helpers for white-box testing.
package zendeskclient

// ExportExtractSubdomain exposes extractSubdomain for testing.
func ExportExtractSubdomain(u string) string { return extractSubdomain(u) }

// ExportJoinInt64s exposes joinInt64s for testing.
func ExportJoinInt64s(ids []int64) string { return joinInt64s(ids) }

// ExportBuildURL exposes buildURL on the httpClient for testing.
func ExportBuildURL(baseURL, path string, params map[string]string) (string, error) {
	c := &httpClient{base: baseURL} // ptrext:allow test-only
	return c.buildURL(path, params)
}
