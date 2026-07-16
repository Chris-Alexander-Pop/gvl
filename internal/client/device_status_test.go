package client

import (
	"testing"
)

func TestDecodeDeviceStatusRejectsOkOnly(t *testing.T) {
	_, err := decodeDeviceStatus([]byte(`{"ok":true}`))
	if err == nil {
		t.Fatal("expected error for legacy ok-only response")
	}
}

func TestDecodeDeviceStatusAcceptsRealStatus(t *testing.T) {
	st, err := decodeDeviceStatus([]byte(`{"onOff":0,"brightness":55,"color":{"r":1,"g":2,"b":3},"colorTemInKelvin":5000}`))
	if err != nil {
		t.Fatal(err)
	}
	if st.Brightness != 55 || st.ColorTemInKelvin != 5000 || st.OnOff != 0 {
		t.Fatalf("unexpected status: %+v", st)
	}
}
