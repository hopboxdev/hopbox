package helper

import (
	"encoding/json"
	"testing"
)

func TestRequestMarshal(t *testing.T) {
	req := Request{
		Action:    ActionConfigureTUN,
		Interface: "utun5",
		LocalIP:   "10.10.0.1",
		PeerIP:    "10.10.0.2",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var got Request
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != req {
		t.Errorf("got %+v, want %+v", got, req)
	}
}

func TestResponseMarshalError(t *testing.T) {
	resp := Response{OK: false, Error: "permission denied"}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var got Response
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.OK || got.Error != "permission denied" {
		t.Errorf("got %+v", got)
	}
}
