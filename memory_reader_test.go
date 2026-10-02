package ip2location_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	ip2location "github.com/ip2location/ip2location-go/v9"
)

type memoryReader struct {
	*bytes.Reader
	data []byte
}

func newMemoryReader(data []byte) *memoryReader {
	return &memoryReader{Reader: bytes.NewReader(data), data: data}
}

func (r *memoryReader) ReadOnlyBytes() []byte { return r.data }
func (r *memoryReader) Close() error {
	r.Reader = bytes.NewReader(nil)
	r.data = nil
	return nil
}

// DB9 with two ranges per address family and strings ending exactly at EOF.
func binFixture(region string) []byte {
	const v4Offset, v4Size, v6Size = 64, 28, 40
	const v6Offset = v4Offset + 3*v4Size
	data := make([]byte, v6Offset+3*v6Size)
	data[0], data[1], data[2], data[29] = 9, 7, 24, 1
	binary.LittleEndian.PutUint32(data[5:], 1)
	binary.LittleEndian.PutUint32(data[9:], v4Offset+1)
	binary.LittleEndian.PutUint32(data[13:], 1)
	binary.LittleEndian.PutUint32(data[17:], v6Offset+1)
	addString := func(value string) uint32 {
		pos := uint32(len(data))
		data = append(data, byte(len(value)))
		data = append(data, value...)
		return pos
	}
	us := addString("US")
	addString("United States") // country long starts three bytes after country short.
	cn := addString("CN")
	addString("China")
	regions := []uint32{addString(region), addString("Beijing")}
	cities := []uint32{addString("Mountain View"), addString("Beijing")}
	zips := []uint32{addString("94043"), addString("100006")}
	countries := []uint32{us, cn}
	for i := 0; i < 2; i++ {
		for _, row := range []int{v4Offset + i*v4Size + 4, v6Offset + i*v6Size + 16} {
			binary.LittleEndian.PutUint32(data[row:], countries[i])
			binary.LittleEndian.PutUint32(data[row+4:], regions[i])
			binary.LittleEndian.PutUint32(data[row+8:], cities[i])
			binary.LittleEndian.PutUint32(data[row+12:], math.Float32bits(37.5))
			binary.LittleEndian.PutUint32(data[row+16:], math.Float32bits(-122.5))
			binary.LittleEndian.PutUint32(data[row+20:], zips[i])
		}
	}
	binary.LittleEndian.PutUint32(data[v4Offset+v4Size:], 1<<31)
	binary.LittleEndian.PutUint32(data[v4Offset+2*v4Size:], math.MaxUint32)
	data[v6Offset+v6Size+15] = 0x80
	for i := 0; i < 16; i++ {
		data[v6Offset+2*v6Size+i] = 0xff
	}
	binary.LittleEndian.PutUint32(data[31:], uint32(len(data)))
	return data
}

func openFileFixture(t *testing.T, data []byte) *ip2location.DB {
	t.Helper()
	file, err := os.CreateTemp("", "ip2location-bin-*")
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	t.Cleanup(func() { os.Remove(name) })
	if _, err = file.Write(data); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := ip2location.OpenDB(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func TestMemoryReaderLookup(t *testing.T) {
	for _, region := range []string{"California", "", strings.Repeat("x", 254)} {
		data := binFixture(region)
		fileDB := openFileFixture(t, data)
		memoryDB, err := ip2location.OpenDBWithReader(newMemoryReader(data))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(memoryDB.Close)
		for _, ip := range []string{"0.0.0.0", "127.255.255.255", "128.0.0.0", "255.255.255.255", "2001:4860::1", "7fff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", "8000::", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", "::ffff:1.2.3.4", "", "invalid"} {
			want, wantErr := fileDB.Get_all(ip)
			got, gotErr := memoryDB.Get_all(ip)
			if !reflect.DeepEqual(got, want) || fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
				t.Fatalf("IP %q: memory=(%+v,%v), file=(%+v,%v)", ip, got, gotErr, want, wantErr)
			}
		}
		got, err := memoryDB.Get_all("1.2.3.4")
		if err != nil || got.Country_short != "US" || got.Country_long != "United States" || got.Region != region || got.City != "Mountain View" || got.Zipcode != "94043" || got.Latitude != 37.5 || got.Longitude != -122.5 {
			t.Fatalf("unexpected first range: %+v, %v", got, err)
		}
		got, err = memoryDB.Get_all("8000::1")
		if err != nil || got.Country_short != "CN" || got.Region != "Beijing" || got.Zipcode != "100006" {
			t.Fatalf("unexpected second range: %+v, %v", got, err)
		}
	}
}

func TestMemoryReaderNilView(t *testing.T) {
	reader := newMemoryReader(binFixture("California"))
	reader.data = nil
	db, err := ip2location.OpenDBWithReader(reader)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.Get_all("200.0.0.1")
	if err != nil || got.Country_short != "CN" || got.Region != "Beijing" || got.City != "Beijing" || got.Zipcode != "100006" {
		t.Fatalf("nil view must preserve normal reader lookup: %+v, %v", got, err)
	}
}

func TestMemoryReaderShortReads(t *testing.T) {
	for _, size := range []int{32, 80, len(binFixture("California")) - 3} {
		data := binFixture("California")[:size]
		fileDB, fileErr := ip2location.OpenDBWithReader(&fileReader{bytes.NewReader(data)})
		memoryDB, memoryErr := ip2location.OpenDBWithReader(newMemoryReader(data))
		if fmt.Sprint(fileErr) != fmt.Sprint(memoryErr) {
			t.Fatalf("size %d: open errors differ: %v, %v", size, fileErr, memoryErr)
		}
		if fileErr != nil {
			if fileErr != io.EOF {
				t.Fatalf("short header: %v", fileErr)
			}
			continue
		}
		t.Cleanup(fileDB.Close)
		t.Cleanup(memoryDB.Close)
		want, wantErr := fileDB.Get_all("200.0.0.1")
		got, gotErr := memoryDB.Get_all("200.0.0.1")
		if !reflect.DeepEqual(got, want) || fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
			t.Fatalf("size %d: short-read results differ: (%+v,%v), (%+v,%v)", size, got, gotErr, want, wantErr)
		}
	}
}

type fileReader struct{ *bytes.Reader }

func (r *fileReader) Close() error { return nil }

func TestMemoryReaderConcurrentQueries(t *testing.T) {
	data := binFixture("California")
	fileDB := openFileFixture(t, data)
	memoryDB, err := ip2location.OpenDBWithReader(newMemoryReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer memoryDB.Close()
	ips := []string{"1.2.3.4", "200.0.0.1", "2001:4860::1", "8000::1"}
	want := make([]ip2location.IP2Locationrecord, len(ips))
	for i, ip := range ips {
		want[i], err = fileDB.Get_all(ip)
		if err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errors := make(chan error, 8)
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < 100; iteration++ {
				for i, ip := range ips {
					got, err := memoryDB.Get_all(ip)
					if err != nil || !reflect.DeepEqual(got, want[i]) {
						errors <- fmt.Errorf("concurrent IP %s: %+v, %v", ip, got, err)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestMemoryReaderResultSurvivesClose(t *testing.T) {
	db, err := ip2location.OpenDBWithReader(newMemoryReader(binFixture("California")))
	if err != nil {
		t.Fatal(err)
	}
	held, err := db.Get_all("1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db = nil
	runtime.GC()
	replacement, err := ip2location.OpenDBWithReader(newMemoryReader(binFixture("Changed")))
	if err != nil {
		t.Fatal(err)
	}
	current, err := replacement.Get_all("1.2.3.4")
	if err != nil || current.Region != "Changed" {
		t.Fatalf("replacement lookup: %+v, %v", current, err)
	}
	replacement.Close()
	runtime.GC()
	if held.Country_short != "US" || held.Country_long != "United States" || held.Region != "California" || held.City != "Mountain View" || held.Zipcode != "94043" {
		t.Fatalf("returned strings changed after close/replacement: %+v", held)
	}
}
