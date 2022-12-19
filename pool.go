package ip2location

import "sync"

var fourBytesPool = sync.Pool{New: func() interface{} {
	return make([]byte, 4)
}}

var emptyFourBytes = make([]byte, 4)

func getFourBytes() []byte {
	return fourBytesPool.Get().([]byte)
}

func putFourBytes(data []byte) {
	copy(data, emptyFourBytes)
	fourBytesPool.Put(data)
}