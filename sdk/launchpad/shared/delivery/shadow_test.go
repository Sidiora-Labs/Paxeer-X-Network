package delivery

import (
	"errors"
	"testing"
)

func TestShadowExactSetAndDuplicateRejection(t *testing.T) {
	a := ShadowRecord{"a", Hash([]byte("a"))}
	b := ShadowRecord{"b", Hash([]byte("b"))}
	d, err := CompareShadow([]ShadowRecord{a, b}, []ShadowRecord{b, a})
	if err != nil || !archiveHash.MatchString(d.Digest) {
		t.Fatal(d, err)
	}
	for _, records := range [][]ShadowRecord{{a}, {a, b, b}, {a, {"b", Hash([]byte("different"))}}, {a, b, {"c", Hash([]byte("extra"))}}} {
		if _, err = CompareShadow([]ShadowRecord{a, b}, records); !errors.Is(err, ErrIntegrity) {
			t.Fatal("shadow mismatch accepted", records, err)
		}
	}
}
