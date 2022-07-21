package ip2location

import (
	"math/big"
	"sync"
)

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

var (
	proxy = bigIntProxy{sync.Pool{New: func() interface{} {
		return big.NewInt(0)
	}}}
	bigInt1 = big.NewInt(1)
)

type bigIntProxy struct {
	pool sync.Pool
}

func (p *bigIntProxy) NewInt(num int64) *big.Int {
	return p.pool.Get().(*big.Int).SetInt64(num)
}

func (p *bigIntProxy) RecycleInt(i *big.Int) {
	if i == nil {
		return
	}
	p.pool.Put(i)
}
