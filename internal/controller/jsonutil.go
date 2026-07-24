package controller

import "encoding/json"

func jsonMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}
