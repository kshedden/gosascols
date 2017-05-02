// GENERATED CODE, DO NOT EDIT
package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/golang/snappy"
)

// handleFixedCol extracts the data for the subject of interest from
// one fixed-width variable.
func handleFixedCol(vn string, dt string, i1, i2 int, bp string) []string {

	f := fmt.Sprintf("%s.bin.sz", vn)
	f = path.Join(bp, f)

	fid, err := os.Open(f)
	if err != nil {
		panic(err)
	}
	defer fid.Close()
	rdr := snappy.NewReader(fid)

	var vals []string
	switch dt {

	case "uint8":
		for j := 0; j < i2; j++ {
			var x uint8
			err := binary.Read(rdr, binary.LittleEndian, &x)
			if err == io.EOF {
				break
			} else if err != nil {
				panic(err)
			}
			if j >= i1 {
				vals = append(vals, fmt.Sprintf("%d", x))
			}
		}

	case "uint16":
		for j := 0; j < i2; j++ {
			var x uint16
			err := binary.Read(rdr, binary.LittleEndian, &x)
			if err == io.EOF {
				break
			} else if err != nil {
				panic(err)
			}
			if j >= i1 {
				vals = append(vals, fmt.Sprintf("%d", x))
			}
		}

	case "uint32":
		for j := 0; j < i2; j++ {
			var x uint32
			err := binary.Read(rdr, binary.LittleEndian, &x)
			if err == io.EOF {
				break
			} else if err != nil {
				panic(err)
			}
			if j >= i1 {
				vals = append(vals, fmt.Sprintf("%d", x))
			}
		}

	case "uint64":
		for j := 0; j < i2; j++ {
			var x uint64
			err := binary.Read(rdr, binary.LittleEndian, &x)
			if err == io.EOF {
				break
			} else if err != nil {
				panic(err)
			}
			if j >= i1 {
				vals = append(vals, fmt.Sprintf("%d", x))
			}
		}

	case "float32":
		for j := 0; j < i2; j++ {
			var x float32
			err := binary.Read(rdr, binary.LittleEndian, &x)
			if err == io.EOF {
				break
			} else if err != nil {
				panic(err)
			}
			if j >= i1 {
				vals = append(vals, fmt.Sprintf("%f", x))
			}
		}

	case "float64":
		for j := 0; j < i2; j++ {
			var x float64
			err := binary.Read(rdr, binary.LittleEndian, &x)
			if err == io.EOF {
				break
			} else if err != nil {
				panic(err)
			}
			if j >= i1 {
				vals = append(vals, fmt.Sprintf("%f", x))
			}
		}

	default:
		panic(fmt.Sprintf("Unknown type %s\n", dt))
	}

	return vals
}

// handleVarCol extracts the data for the subject of interest from one
// variable-width variable.
func handleVarCol(vn string, dt string, i1, i2 int, bp string, codes map[int]string) []string {

	f := fmt.Sprintf("%s.bin.sz", vn)
	f = path.Join(bp, f)

	fid, err := os.Open(f)
	if err != nil {
		panic(err)
	}
	defer fid.Close()
	rdr := snappy.NewReader(fid)
	br := bufio.NewReader(rdr)

	var vals []string
	switch dt {
	case "uvarint":
		for j := 0; j < i2; j++ {
			x, err := binary.ReadUvarint(br)
			if err == io.EOF {
				break
			} else if err != nil {
				panic(err)
			}
			if j >= i1 {
				vals = append(vals, codes[int(x)])
			}
		}
	default:
		panic(fmt.Sprintf("Unknown type %s\n", dt))
	}

	return vals
}
