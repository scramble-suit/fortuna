package fortuna

import (
	"crypto/cipher"

	"github.com/seehuhn/sha256d"
	"github.com/seehuhn/trace"
)

const (
	// maxBlocks gives the maximal number of blocks to generate until
	// rekeying is required.
	maxBlocks = 1 << 16
)

// NewCipher is the type which represents the function to allocate a
// new block cipher.  A typical example of a function of this type is
// aes.NewCipher.
type NewCipher func([]byte) (cipher.Block, error)

// Generator holds the state of one instance of the Fortuna pseudo
// random number generator.  Before use, the generator must be seeded
// using the Reseed() or Seed() method.  Randomness can then be
// extracted using the PseudoRandomData() method.  The Generator class
// implements the rand.Source interface.
//
// This Generator class is not safe for use with concurrent accesss.
// If the generator is used from different Go-routines, the caller
// must synchronise accesses using sync.Mutex or similar.
type Generator struct {
	newCipher NewCipher
	key       []byte
	cipher    cipher.Block
	counter   []byte
}

func (gen *Generator) inc() {
	// The counter is stored least-signigicant byte first.
	for i := 0; i < len(gen.counter); i++ {
		gen.counter[i]++
		if gen.counter[i] != 0 {
			break
		}
	}
}

func (gen *Generator) setKey(key []byte) {
	gen.key = key
	cipher, err := gen.newCipher(gen.key)
	if err != nil {
		panic("newCipher() failed, cannot set generator key")
	}
	gen.cipher = cipher
}

// NewGenerator creates a new instance of the Fortuna random number
// generator.  The function newCipher should normally be aes.NewCipher
// from the crypto/aes package, but the Serpent or Twofish ciphers can
// also be used.
func NewGenerator(newCipher NewCipher) *Generator {
	gen := &Generator{
		newCipher: newCipher,
	}
	initialKey := make([]byte, sha256d.Size)
	gen.setKey(initialKey)
	gen.counter = make([]byte, gen.cipher.BlockSize())

	return gen
}

// Seed uses the current generator state and the given seed value to
// update the generator state.  Care is taken to make sure that
// knowledge of the new state after a reseed does not allow to
// reconstruct previous output values of the generator.
func (gen *Generator) Reseed(seed []byte) {
	trace.T("fortuna/generator", trace.PrioDebug, "setting the PRNG seed")
	hash := sha256d.New()
	hash.Write(gen.key)
	hash.Write(seed)
	gen.setKey(hash.Sum(nil))
	gen.inc()
}

func isZero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

// generateBlocks appends k blocks of random bits to data and returns
// the resulting slice.  The size of a block is given by the block
// size of the underlying cipher, i.e. 16 bytes for AES.
func (gen *Generator) generateBlocks(data []byte, k uint) []byte {
	if isZero(gen.counter) {
		panic("generator not yet seeded")
	}

	counterSize := uint(len(gen.counter))
	buf := make([]byte, counterSize)
	for i := uint(0); i < k; i++ {
		gen.cipher.Encrypt(buf, gen.counter)
		data = append(data, buf...)
		gen.inc()
	}

	return data
}

func (gen *Generator) numBlocks(n uint) uint {
	k := uint(len(gen.counter))
	return (n + k - 1) / k
}

// PseudoRandomData returns a slice of n pseudo-random bytes.  The
// result can be used as a replacement for a sequence of uniformly
// distributed and independent bytes.
func (gen *Generator) PseudoRandomData(n uint) []byte {
	numBlocks := gen.numBlocks(n)
	res := make([]byte, 0, numBlocks*uint(len(gen.counter)))

	for numBlocks > 0 {
		count := numBlocks
		if count > maxBlocks {
			count = maxBlocks
		}
		res = gen.generateBlocks(res, count)
		numBlocks -= count

		keySize := uint(len(gen.key))
		newKey := gen.generateBlocks(nil, gen.numBlocks(keySize))
		gen.setKey(newKey[:keySize])
	}

	trace.T("fortuna/generator", trace.PrioDebug,
		"generated %d pseudo-random bytes", n)
	return res[:n]
}

func bytesToInt64(bytes []byte) int64 {
	var res int64
	res = int64(bytes[0])
	for _, x := range bytes[1:] {
		res = res<<8 | int64(x)
	}
	return res
}

// Int63 returns a positive random integer, uniformly distributed on
// the range 0, 1, ..., 2^63-1.  This function is part of the
// rand.Source interface.
func (gen *Generator) Int63() int64 {
	bytes := gen.PseudoRandomData(8)
	bytes[0] &= 0x7f
	return bytesToInt64(bytes)
}

func int64ToBytes(x int64) []byte {
	bytes := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		bytes[i] = byte(x & 0xff)
		x = x >> 8
	}
	return bytes
}

// Seed uses the given seed value to set a new generator state.  In
// contrast to the Reseed() method, the Seed() method discards the
// previous state, thus allowing to generate reproducible output.
// This function is part of the rand.Source interface.
func (gen *Generator) Seed(seed int64) {
	bytes := int64ToBytes(seed)
	gen.key = make([]byte, len(gen.key))
	gen.Reseed(bytes)
}
