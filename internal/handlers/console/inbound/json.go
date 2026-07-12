// SPDX-License-Identifier: Apache-2.0

package inbound

import "encoding/json"

func marshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

var jsonMarshal func(any) ([]byte, error)

func init() {
	jsonMarshal = marshalJSON
}
