// GENERATED CODE, DO NOT EDIT
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/adler32"
	"io"
	"log"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/golang/snappy"
	"github.com/kshedden/datareader"
	"github.com/kshedden/gosascols/config"
)

var (
	// Needed to avoid errrors if no other references are made to these packages.
	_ = strings.TrimSpace
	_ = strconv.Atoi

	conf *config.Config

	rslt_chan chan *rec

	dtypes = `{"Copay":"float32","Dstatus":"uint8","Dx1":"string","Dx2":"string","Enrolid":"uint64","Netpay":"float32","Pay":"float32","Proc1":"string","Seqnum":"uint64","Stdprov":"uint16","Svcdate":"uint16"}
`

	wg  sync.WaitGroup
	hwg sync.WaitGroup

	sem chan bool

	buckets []*Bucket

	logger *log.Logger
)

func setupLogger() {

	fn := "sastocols_" + path.Base(conf.TargetDir) + ".log"
	fid, err := os.Create(fn)
	if err != nil {
		panic(err)
	}

	logger = log.New(fid, "", log.Ltime)
}

// harvest retrieves data from the producers in the form of data
// records, and adds each record to the appropriate bucket.
func harvest() {

	ha := adler32.New()

	buf := make([]byte, 8)

	for r := range rslt_chan {

		binary.LittleEndian.PutUint64(buf, r.Enrolid)
		ha.Reset()
		_, err := ha.Write(buf)
		if err != nil {
			panic(err)
		}

		bucket := int(ha.Sum32() % conf.NumBuckets)
		buckets[bucket].Add(r)
	}

	hwg.Done()
}

// sendrecs drains a chunk, sending each record in the chunk to be
// harvested.
func sendrecs(c *chunk) {

	defer func() { <-sem; wg.Done() }()

	for {
		r := c.nextrec()
		if r == nil {
			break
		}
		rslt_chan <- r
	}
}

// dofile processes one SAS file.
func dofile(filename string) {

	defer func() { wg.Done() }()

	logger.Printf("Starting file %s", filename)

	fid, err := os.Open(filename)
	if err != nil {
		panic(err)
	}
	defer fid.Close()

	sas, err := datareader.NewSAS7BDATReader(fid)
	if err != nil {
		panic(err)
	}
	sas.TrimStrings = true

	logger.Printf("%s has %d rows", filename, sas.RowCount())

	cm := make(map[string]int)
	for k, na := range sas.ColumnNames() {
		cm[na] = k
	}

	for chunk_id := 0; ; chunk_id++ {

		logger.Printf("Starting chunk %d", chunk_id)
		if conf.MaxChunk > 0 && chunk_id > int(conf.MaxChunk) {
			logger.Printf("Read %d blocks from %s, breaking early", chunk_id, filename)
			break
		}

		chunk := new(chunk)

		data, err := sas.Read(int(conf.SASChunkSize))
		if data == nil {
			break
		}
		if err != nil {
			panic(err)
		}

		err = chunk.getcols(data, cm)
		if err != nil {
			print("In file ", filename, "\n\n")
			panic(err)
		}

		wg.Add(1)
		sem <- true
		go sendrecs(chunk)
	}
}

// nextrec finds the next valid record from the chunk and returns it.
// recs with missing enrolid values are skipped, so this may not
// return a value for every row of the SAS file chunk.  Returns nil
// when the chunk is fully processed.
func (c *chunk) nextrec() *rec {

	for {
		rec, cont := c.trynextrec()
		if rec != nil {
			return rec
		}
		if !cont {
			break
		}
	}

	return nil
}

// writeconfig writes the configuration information for the gocols dataset.  This
// configuration information is intended for users of the target dataset so does
// not need to contain information about how the data were derived from the source
// SAS files.
func writeconfig() {

	type Config struct {
		NumBuckets  uint32
		Compression string
		CodesDir    string
	}

	c := Config{NumBuckets: conf.NumBuckets, Compression: "snappy", CodesDir: conf.CodesDir}

	fid, err := os.Create(path.Join(conf.TargetDir, "conf.json"))
	if err != nil {
		panic(err)
	}
	defer fid.Close()
	enc := json.NewEncoder(fid)
	err = enc.Encode(c)
	if err != nil {
		panic(err)
	}
}

func setup() {

	rslt_chan = make(chan *rec)
	sem = make(chan bool, conf.Concurrency)

	buckets = make([]*Bucket, conf.NumBuckets)
	for i, _ := range buckets {
		buckets[i] = new(Bucket)
		buckets[i].BucketNum = uint32(i)
		buckets[i].Conf = conf
	}

	err := os.MkdirAll(conf.TargetDir, 0755)
	if err != nil {
		panic(err)
	}

	writeconfig()

	fn := path.Join(conf.TargetDir, "Buckets")
	err = os.RemoveAll(fn)
	if err != nil {
		panic(err)
	}

	pa := path.Join(conf.TargetDir, "Buckets")
	os.MkdirAll(pa, 0755)
	for k := 0; k < int(conf.NumBuckets); k++ {
		bns := fmt.Sprintf("%04d", k)
		dn := path.Join(conf.TargetDir, "Buckets", bns)
		err = os.MkdirAll(dn, 0755)
		if err != nil {
			panic(err)
		}

		fn := path.Join(dn, "dtypes.json")
		fid, err := os.Create(fn)
		if err != nil {
			panic(err)
		}
		_, err = fid.Write([]byte(dtypes))
		if err != nil {
			panic(err)
		}
	}
}

func Run(cnf *config.Config, lgr *log.Logger) {

	logger = lgr
	conf = cnf

	setup()

	hwg.Add(1)
	go harvest()

	for _, fn := range conf.SASFiles {
		fn = path.Join(conf.SourceDir, fn)
		wg.Add(1)
		go dofile(fn)
	}

	wg.Wait()
	close(rslt_chan)
	hwg.Wait()

	for k := 0; k < int(conf.NumBuckets); k++ {
		buckets[k].Flush()
	}

	logger.Printf("All done")
}

// rec is a row that will be added to a Bucket.
type rec struct {
	Copay   float32
	Dstatus uint8
	Dx1     string
	Dx2     string
	Enrolid uint64
	Netpay  float32
	Pay     float32
	Proc1   string
	Seqnum  uint64
	Stdprov uint16
	Svcdate uint16
}

// Bucket is a memory-backed container for columnized data.  It
// contains data exactly as it will be written to disk.
type Bucket struct {
	BaseBucket

	code    []uint16
	Copay   []float32
	Dstatus []uint8
	Dx1     []string
	Dx2     []string
	Enrolid []uint64
	Netpay  []float32
	Pay     []float32
	Proc1   []string
	Seqnum  []uint64
	Stdprov []uint16
	Svcdate []uint16
}

// chunk is a typed container for data pulled directly out of a SAS file.
// There are no type conversions or other modifications from the SAS file.
type chunk struct {
	row      int
	col      int
	Copay    []float64
	Copaym   []bool
	Dstatus  []string
	Dstatusm []bool
	Dx1      []string
	Dx1m     []bool
	Dx2      []string
	Dx2m     []bool
	Enrolid  []float64
	Enrolidm []bool
	Netpay   []float64
	Netpaym  []bool
	Pay      []float64
	Paym     []bool
	Proc1    []string
	Proc1m   []bool
	Seqnum   []float64
	Seqnumm  []bool
	Stdprov  []float64
	Stdprovm []bool
	Svcdate  []float64
	Svcdatem []bool
}

// Add appends a rec to the end of the Bucket.
func (bucket *Bucket) Add(r *rec) {

	bucket.Mut.Lock()

	bucket.Copay = append(bucket.Copay, r.Copay)
	bucket.Dstatus = append(bucket.Dstatus, r.Dstatus)
	bucket.Dx1 = append(bucket.Dx1, r.Dx1)
	bucket.Dx2 = append(bucket.Dx2, r.Dx2)
	bucket.Enrolid = append(bucket.Enrolid, r.Enrolid)
	bucket.Netpay = append(bucket.Netpay, r.Netpay)
	bucket.Pay = append(bucket.Pay, r.Pay)
	bucket.Proc1 = append(bucket.Proc1, r.Proc1)
	bucket.Seqnum = append(bucket.Seqnum, r.Seqnum)
	bucket.Stdprov = append(bucket.Stdprov, r.Stdprov)
	bucket.Svcdate = append(bucket.Svcdate, r.Svcdate)

	bucket.Mut.Unlock()

	if uint64(len(bucket.Enrolid)) > conf.BufMaxRecs {
		bucket.Flush()
	}
}

// Flush writes all the data from the Bucket to disk.
func (bucket *Bucket) Flush() {

	logger.Printf("Flushing bucket %d", bucket.BucketNum)

	bucket.Mut.Lock()

	bucket.flushfloat32("Copay", bucket.Copay)
	bucket.Copay = bucket.Copay[0:0]
	bucket.flushuint8("Dstatus", bucket.Dstatus)
	bucket.Dstatus = bucket.Dstatus[0:0]
	bucket.flushstring("Dx1", bucket.Dx1)
	bucket.Dx1 = bucket.Dx1[0:0]
	bucket.flushstring("Dx2", bucket.Dx2)
	bucket.Dx2 = bucket.Dx2[0:0]
	bucket.flushuint64("Enrolid", bucket.Enrolid)
	bucket.Enrolid = bucket.Enrolid[0:0]
	bucket.flushfloat32("Netpay", bucket.Netpay)
	bucket.Netpay = bucket.Netpay[0:0]
	bucket.flushfloat32("Pay", bucket.Pay)
	bucket.Pay = bucket.Pay[0:0]
	bucket.flushstring("Proc1", bucket.Proc1)
	bucket.Proc1 = bucket.Proc1[0:0]
	bucket.flushuint64("Seqnum", bucket.Seqnum)
	bucket.Seqnum = bucket.Seqnum[0:0]
	bucket.flushuint16("Stdprov", bucket.Stdprov)
	bucket.Stdprov = bucket.Stdprov[0:0]
	bucket.flushuint16("Svcdate", bucket.Svcdate)
	bucket.Svcdate = bucket.Svcdate[0:0]

	bucket.Mut.Unlock()
}

// getcols fills a chunk with data from a SAS file.
func (c *chunk) getcols(data []*datareader.Series, cm map[string]int) error {

	var err error
	var ii int
	var ok bool

	ii, ok = cm["COPAY"]
	if ok {
		c.Copay, c.Copaym, err = data[ii].AsFloat64Slice()
		if err != nil {
			panic(err)
		}

	} else {
		msg := fmt.Sprintf("Variable COPAY required but not found in SAS file\n")
		return fmt.Errorf(msg)
	}

	ii, ok = cm["DSTATUS"]
	if ok {
		c.Dstatus, c.Dstatusm, err = data[ii].AsStringSlice()
		if err != nil {
			panic(err)
		}

	} else {
		msg := fmt.Sprintf("Variable DSTATUS required but not found in SAS file\n")
		return fmt.Errorf(msg)
	}

	ii, ok = cm["DX1"]
	if ok {
		c.Dx1, c.Dx1m, err = data[ii].AsStringSlice()
		if err != nil {
			panic(err)
		}

	} else {
		msg := fmt.Sprintf("Variable DX1 required but not found in SAS file\n")
		return fmt.Errorf(msg)
	}

	ii, ok = cm["DX2"]
	if ok {
		c.Dx2, c.Dx2m, err = data[ii].AsStringSlice()
		if err != nil {
			panic(err)
		}

	} else {
		msg := fmt.Sprintf("Variable DX2 required but not found in SAS file\n")
		return fmt.Errorf(msg)
	}

	ii, ok = cm["ENROLID"]
	if ok {
		c.Enrolid, c.Enrolidm, err = data[ii].AsFloat64Slice()
		if err != nil {
			panic(err)
		}

	} else {
		msg := fmt.Sprintf("Variable ENROLID required but not found in SAS file\n")
		return fmt.Errorf(msg)
	}

	ii, ok = cm["NETPAY"]
	if ok {
		c.Netpay, c.Netpaym, err = data[ii].AsFloat64Slice()
		if err != nil {
			panic(err)
		}

	} else {
		msg := fmt.Sprintf("Variable NETPAY required but not found in SAS file\n")
		return fmt.Errorf(msg)
	}

	ii, ok = cm["PAY"]
	if ok {
		c.Pay, c.Paym, err = data[ii].AsFloat64Slice()
		if err != nil {
			panic(err)
		}

	} else {
		msg := fmt.Sprintf("Variable PAY required but not found in SAS file\n")
		return fmt.Errorf(msg)
	}

	ii, ok = cm["PROC1"]
	if ok {
		c.Proc1, c.Proc1m, err = data[ii].AsStringSlice()
		if err != nil {
			panic(err)
		}

	} else {
		msg := fmt.Sprintf("Variable PROC1 required but not found in SAS file\n")
		return fmt.Errorf(msg)
	}

	ii, ok = cm["SEQNUM"]
	if ok {
		c.Seqnum, c.Seqnumm, err = data[ii].AsFloat64Slice()
		if err != nil {
			panic(err)
		}

	} else {
		msg := fmt.Sprintf("Variable SEQNUM required but not found in SAS file\n")
		return fmt.Errorf(msg)
	}

	ii, ok = cm["STDPROV"]
	if ok {
		c.Stdprov, c.Stdprovm, err = data[ii].AsFloat64Slice()
		if err != nil {
			panic(err)
		}

	} else {
		msg := fmt.Sprintf("Variable STDPROV required but not found in SAS file\n")
		return fmt.Errorf(msg)
	}

	ii, ok = cm["SVCDATE"]
	if ok {
		c.Svcdate, c.Svcdatem, err = data[ii].AsFloat64Slice()
		if err != nil {
			panic(err)
		}

	} else {
		msg := fmt.Sprintf("Variable SVCDATE required but not found in SAS file\n")
		return fmt.Errorf(msg)
	}

	return nil
}

func (c *chunk) trynextrec() (*rec, bool) {

	if c.row >= len(c.Enrolid) {
		return nil, false
	}

	r := new(rec)

	i := c.row

	if c.Enrolidm[i] {
		c.row++
		return nil, true
	}

	r.Copay = float32(c.Copay[i])

	// Convert string to number
	if len(c.Dstatus[i]) > 0 {
		x, err := strconv.Atoi(c.Dstatus[i])
		if err == nil {
			r.Dstatus = uint8(x)
		}
	}

	r.Dx1 = strings.TrimSpace(c.Dx1[i])

	r.Dx2 = strings.TrimSpace(c.Dx2[i])

	r.Enrolid = uint64(c.Enrolid[i])

	r.Netpay = float32(c.Netpay[i])

	r.Pay = float32(c.Pay[i])

	r.Proc1 = strings.TrimSpace(c.Proc1[i])

	r.Seqnum = uint64(c.Seqnum[i])

	r.Stdprov = uint16(c.Stdprov[i])

	r.Svcdate = uint16(c.Svcdate[i])

	c.row++

	return r, true
}

type BaseBucket struct {

	// The number of the bucket, corresponds to the file name in
	// the Buckets directory.
	BucketNum uint32

	// Locks for accessing the bucket's data.
	Mut sync.Mutex

	Conf *config.Config
}

// openfile opens a file for appending data in the bucket's directory.
func (bucket *BaseBucket) openfile(varname string) (io.Closer, io.WriteCloser) {

	bp := config.BucketPath(int(bucket.BucketNum), bucket.Conf)
	fn := path.Join(bp, varname+".bin.sz")
	fid, err := os.OpenFile(fn, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}

	gid := snappy.NewBufferedWriter(fid)

	return fid, gid
}

func (bucket *BaseBucket) flushstring(varname string, vec []string) {

	toclose, wtr := bucket.openfile(varname)

	nl := []byte("\n")
	for _, x := range vec {
		_, err := wtr.Write([]byte(x))
		if err != nil {
			panic(err)
		}
		_, err = wtr.Write(nl)
		if err != nil {
			panic(err)
		}
	}

	err := wtr.Close()
	if err != nil {
		panic(err)
	}
	err = toclose.Close()
	if err != nil {
		panic(err)
	}
}
func (bucket *BaseBucket) flushuint8(varname string, vec []uint8) {

	toclose, wtr := bucket.openfile(varname)

	for _, x := range vec {
		err := binary.Write(wtr, binary.LittleEndian, x)
		if err != nil {
			panic(err)
		}
	}

	err := wtr.Close()
	if err != nil {
		panic(err)
	}
	err = toclose.Close()
	if err != nil {
		panic(err)
	}
}
func (bucket *BaseBucket) flushuint16(varname string, vec []uint16) {

	toclose, wtr := bucket.openfile(varname)

	for _, x := range vec {
		err := binary.Write(wtr, binary.LittleEndian, x)
		if err != nil {
			panic(err)
		}
	}

	err := wtr.Close()
	if err != nil {
		panic(err)
	}
	err = toclose.Close()
	if err != nil {
		panic(err)
	}
}
func (bucket *BaseBucket) flushuint32(varname string, vec []uint32) {

	toclose, wtr := bucket.openfile(varname)

	for _, x := range vec {
		err := binary.Write(wtr, binary.LittleEndian, x)
		if err != nil {
			panic(err)
		}
	}

	err := wtr.Close()
	if err != nil {
		panic(err)
	}
	err = toclose.Close()
	if err != nil {
		panic(err)
	}
}
func (bucket *BaseBucket) flushuint64(varname string, vec []uint64) {

	toclose, wtr := bucket.openfile(varname)

	for _, x := range vec {
		err := binary.Write(wtr, binary.LittleEndian, x)
		if err != nil {
			panic(err)
		}
	}

	err := wtr.Close()
	if err != nil {
		panic(err)
	}
	err = toclose.Close()
	if err != nil {
		panic(err)
	}
}
func (bucket *BaseBucket) flushfloat32(varname string, vec []float32) {

	toclose, wtr := bucket.openfile(varname)

	for _, x := range vec {
		err := binary.Write(wtr, binary.LittleEndian, x)
		if err != nil {
			panic(err)
		}
	}

	err := wtr.Close()
	if err != nil {
		panic(err)
	}
	err = toclose.Close()
	if err != nil {
		panic(err)
	}
}
func (bucket *BaseBucket) flushfloat64(varname string, vec []float64) {

	toclose, wtr := bucket.openfile(varname)

	for _, x := range vec {
		err := binary.Write(wtr, binary.LittleEndian, x)
		if err != nil {
			panic(err)
		}
	}

	err := wtr.Close()
	if err != nil {
		panic(err)
	}
	err = toclose.Close()
	if err != nil {
		panic(err)
	}
}

func main() {

	if len(os.Args) != 2 {
		os.Stderr.WriteString("sastocols: Wrong number of arguments\n\n")
		msg := fmt.Sprintf("Usage: %s config.toml\n\n", os.Args[0])
		os.Stderr.WriteString(msg)
		os.Exit(1)
	}

	conf = config.ReadConfig(os.Args[1])
	setupLogger()
	logger.Printf("Read config from %s", os.Args[1])

	Run(conf, logger)

	logger.Printf("Finished, exiting")
}
