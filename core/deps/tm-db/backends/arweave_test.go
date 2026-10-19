package backends

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func NewMockArweaveDB(indexList [][]byte, txDataList [][]byte, txIndices []int) *ArweaveDB {
	indexByVersion := map[int][]byte{}
	txDataByTxId := map[string][]byte{}
	for version, index := range indexList {
		indexTxId := intToBase64Sha256(len(txIndices) + version)
		txDataByTxId[indexTxId] = index
		indexByVersion[version] = []byte(indexTxId)
	}
	for i, txData := range txDataList {
		txDataByTxId[intToBase64Sha256(txIndices[i])] = txData
	}

	return &ArweaveDB{
		txDataByIdGetter: func(txId []byte) ([]byte, error) {
			if txData, ok := txDataByTxId[string(txId)]; ok {
				return txData, nil
			} else {
				return nil, &ErrKeyNotFound{}
			}
		},
		versionTxIdGetter: func(version []byte) ([]byte, error) {
			versionInt := int(binary.BigEndian.Uint64(version))
			if index, ok := indexByVersion[versionInt]; ok {
				return index, nil
			} else {
				return nil, &ErrKeyNotFound{}
			}
		},
		closer: func() error { return nil },
	}
}

func mockIndex(prefixes []string, indices []int) []byte {
	index := []byte{}
	for i, prefix := range prefixes {
		entry := append(padZeroes(prefix), []byte(intToBase64Sha256(indices[i]))...)
		index = append(index, entry...)
	}
	return index
}

func mockTxData(keys []string, values []string) []byte {
	m := map[string]string{}
	for i, key := range keys {
		m[key] = values[i]
	}
	if data, err := json.Marshal(&m); err != nil {
		panic(err)
	} else {
		return data
	}
}

func padZeroes(prefix string) []byte {
	bz := make([]byte, IndexKeyPrefixLen)
	copy(bz, []byte(prefix))
	return bz
}

func intToBase64Sha256(i int) string {
	id := make([]byte, 4)
	binary.BigEndian.PutUint32(id, uint32(i))
	idsha256 := sha256.Sum256(id)
	return base64.StdEncoding.EncodeToString(idsha256[:])
}

func TestGetIndexEntries(t *testing.T) {
	index := mockIndex([]string{"ab", "cd"}, []int{0, 1})
	entries := getIndexEntries("cc", index)
	require.Equal(t, 1, len(entries))
	require.Equal(t, string(padZeroes("cd")), entries[0].keyPrefix)
	require.Equal(t, intToBase64Sha256(1), string(entries[0].txId))

	entries = getIndexEntries("aab", index)
	require.Equal(t, 1, len(entries))
	require.Equal(t, string(padZeroes("ab")), entries[0].keyPrefix)
	require.Equal(t, intToBase64Sha256(0), string(entries[0].txId))

	entries = getIndexEntries("abc", index)
	require.Equal(t, 1, len(entries))
	require.Equal(t, string(padZeroes("cd")), entries[0].keyPrefix)
	require.Equal(t, intToBase64Sha256(1), string(entries[0].txId))

	// doesn't exist
	entries = getIndexEntries("cde", index)
	require.Equal(t, 0, len(entries))

	// multiple TXs for the same prefix
	index = mockIndex([]string{"ab", "cd", "cd", "ce"}, []int{0, 1, 2, 3})
	entries = getIndexEntries("cc", index)
	require.Equal(t, 2, len(entries))
	require.Equal(t, string(padZeroes("cd")), entries[0].keyPrefix)
	require.Equal(t, intToBase64Sha256(1), string(entries[0].txId))
	require.Equal(t, string(padZeroes("cd")), entries[1].keyPrefix)
	require.Equal(t, intToBase64Sha256(2), string(entries[1].txId))
}

func TestGetIndexEntriesForRange(t *testing.T) {
	// empty
	index := mockIndex([]string{}, []int{})
	entries := getIndexEntriesForRange("a", "b", index)
	require.Equal(t, 0, len(entries))

	// single TX
	index = mockIndex([]string{"ab"}, []int{0})
	entries = getIndexEntriesForRange("aa", "ac", index)
	require.Equal(t, 1, len(entries))
	entries = getIndexEntriesForRange("ab", "ac", index)
	require.Equal(t, 1, len(entries))
	entries = getIndexEntriesForRange("ac", "ad", index)
	require.Equal(t, 0, len(entries))

	// multiple TX
	index = mockIndex([]string{"ab", "cd", "cd", "ce"}, []int{0, 1, 2, 3})
	entries = getIndexEntriesForRange("ab", "cd", index)
	require.Equal(t, 3, len(entries))
	entries = getIndexEntriesForRange("ab", "ce", index)
	require.Equal(t, 4, len(entries))
	entries = getIndexEntriesForRange("cd", "cf", index)
	require.Equal(t, 3, len(entries))
	entries = getIndexEntriesForRange("aa", "cf", index)
	require.Equal(t, 4, len(entries))
}

func TestGet(t *testing.T) {
	indexV0 := mockIndex([]string{"ab", "cd", "cd", "ce"}, []int{0, 1, 2, 3})
	indexV1 := mockIndex([]string{"ac", "cd", "ce"}, []int{4, 5, 6})
	txData := [][]byte{
		mockTxData([]string{"aa"}, []string{"v1"}),
		mockTxData([]string{"cc"}, []string{"v2"}),
		mockTxData([]string{"cd"}, []string{"v3"}),
		mockTxData([]string{"ce"}, []string{"v4"}),
		mockTxData([]string{"ac"}, []string{"v5"}),
		mockTxData([]string{"cc"}, []string{"v6"}),
		mockTxData([]string{"ce"}, []string{"v7"}),
	}
	mockDB := NewMockArweaveDB([][]byte{indexV0, indexV1}, txData, []int{0, 1, 2, 3, 4, 5, 6})
	v0Bz, v1Bz := make([]byte, 8), make([]byte, 8)
	binary.BigEndian.PutUint64(v0Bz, 0)
	binary.BigEndian.PutUint64(v1Bz, 1)
	value, err := mockDB.Get(append(v0Bz, []byte("aa")...))
	require.Nil(t, err)
	require.Equal(t, "v1", string(value))

	value, err = mockDB.Get(append(v0Bz, []byte("ac")...))
	require.Equal(t, &ErrKeyNotFound{key: "ac"}, err)

	value, err = mockDB.Get(append(v0Bz, []byte("cc")...))
	require.Nil(t, err)
	require.Equal(t, "v2", string(value))

	value, err = mockDB.Get(append(v0Bz, []byte("cd")...))
	require.Nil(t, err)
	require.Equal(t, "v3", string(value))

	value, err = mockDB.Get(append(v0Bz, []byte("ce")...))
	require.Nil(t, err)
	require.Equal(t, "v4", string(value))

	value, err = mockDB.Get(append(v1Bz, []byte("ac")...))
	require.Nil(t, err)
	require.Equal(t, "v5", string(value))

	value, err = mockDB.Get(append(v1Bz, []byte("cc")...))
	require.Nil(t, err)
	require.Equal(t, "v6", string(value))

	value, err = mockDB.Get(append(v1Bz, []byte("ce")...))
	require.Nil(t, err)
	require.Equal(t, "v7", string(value))
}

func TestHas(t *testing.T) {
	indexV0 := mockIndex([]string{"ab", "cd", "cd", "ce"}, []int{0, 1, 2, 3})
	indexV1 := mockIndex([]string{"ac", "cd", "ce"}, []int{4, 5, 6})
	txData := [][]byte{
		mockTxData([]string{"aa"}, []string{"v1"}),
		mockTxData([]string{"cc"}, []string{"v2"}),
		mockTxData([]string{"cd"}, []string{"v3"}),
		mockTxData([]string{"ce"}, []string{"v4"}),
		mockTxData([]string{"ac"}, []string{"v5"}),
		mockTxData([]string{"cc"}, []string{"v6"}),
		mockTxData([]string{"ce"}, []string{"v7"}),
	}
	mockDB := NewMockArweaveDB([][]byte{indexV0, indexV1}, txData, []int{0, 1, 2, 3, 4, 5, 6})
	v0Bz, v1Bz := make([]byte, 8), make([]byte, 8)
	binary.BigEndian.PutUint64(v0Bz, 0)
	binary.BigEndian.PutUint64(v1Bz, 1)
	exists, err := mockDB.Has(append(v0Bz, []byte("aa")...))
	require.Nil(t, err)
	require.True(t, exists)

	exists, err = mockDB.Has(append(v0Bz, []byte("ac")...))
	require.Nil(t, err)
	require.False(t, exists)

	exists, err = mockDB.Has(append(v0Bz, []byte("cc")...))
	require.Nil(t, err)
	require.True(t, exists)

	exists, err = mockDB.Has(append(v0Bz, []byte("cd")...))
	require.Nil(t, err)
	require.True(t, exists)

	exists, err = mockDB.Has(append(v0Bz, []byte("ce")...))
	require.Nil(t, err)
	require.True(t, exists)

	exists, err = mockDB.Has(append(v1Bz, []byte("ac")...))
	require.Nil(t, err)
	require.True(t, exists)
}

func TestIterator(t *testing.T) {
	indexV0 := mockIndex([]string{"ab", "cd", "cd", "ce"}, []int{0, 1, 2, 3})
	indexV1 := mockIndex([]string{"ac", "cd", "ce"}, []int{4, 5, 6})
	txData := [][]byte{
		mockTxData([]string{"aa"}, []string{"v1"}),
		mockTxData([]string{"cc"}, []string{"v2"}),
		mockTxData([]string{"cd"}, []string{"v3"}),
		mockTxData([]string{"ce"}, []string{"v4"}),
		mockTxData([]string{"ac"}, []string{"v5"}),
		mockTxData([]string{"cc"}, []string{"v6"}),
		mockTxData([]string{"ce"}, []string{"v7"}),
	}
	mockDB := NewMockArweaveDB([][]byte{indexV0, indexV1}, txData, []int{0, 1, 2, 3, 4, 5, 6})
	v0Bz, v1Bz := make([]byte, 8), make([]byte, 8)
	binary.BigEndian.PutUint64(v0Bz, 0)
	binary.BigEndian.PutUint64(v1Bz, 1)

	tester := func(start string, end string, keys []string, vals []string) {
		iterator, err := mockDB.Iterator(append(v0Bz, []byte(start)...), append(v0Bz, []byte(end)...))
		require.Nil(t, err)
		reviterator, err := mockDB.ReverseIterator(append(v0Bz, []byte(start)...), append(v0Bz, []byte(end)...))
		require.Nil(t, err)
		require.Nil(t, err)
		for i, key := range keys {
			require.Equal(t, key, string(iterator.Key()))
			require.Equal(t, vals[i], string(iterator.Value()))
			iterator.Next()
			require.Equal(t, keys[len(keys)-1-i], string(reviterator.Key()))
			require.Equal(t, vals[len(keys)-1-i], string(reviterator.Value()))
			reviterator.Next()
		}
		require.False(t, iterator.Valid())
		require.False(t, reviterator.Valid())
	}
	tester("a", "cc", []string{"aa"}, []string{"v1"})
	tester("aa", "cd", []string{"aa", "cc"}, []string{"v1", "v2"})
	tester("aa", "ce", []string{"aa", "cc", "cd"}, []string{"v1", "v2", "v3"})
	tester("aa", "cea", []string{"aa", "cc", "cd", "ce"}, []string{"v1", "v2", "v3", "v4"})
}
