package sign

import "testing"

func TestSignVerify(t *testing.T) {
	pub, seed, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"app_version":"1.0.0"}`)
	sig, err := Sign(seed, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(pub, data, sig); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := Verify(pub, []byte(`{"app_version":"9.9.9"}`), sig); err == nil {
		t.Fatal("tampered data accepted")
	}
	other, _, _ := GenerateKey()
	if err := Verify(other, data, sig); err == nil {
		t.Fatal("wrong key accepted")
	}
	if err := Verify("", data, sig); err == nil {
		t.Fatal("empty key accepted")
	}
}
