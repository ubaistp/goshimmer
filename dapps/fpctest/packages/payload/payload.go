package payload

import (
	"sync"

	"github.com/iotaledger/goshimmer/packages/binary/messagelayer/payload"
	"github.com/iotaledger/hive.go/marshalutil"
	"github.com/iotaledger/hive.go/objectstorage"
	"github.com/iotaledger/hive.go/stringify"
	"golang.org/x/crypto/blake2b"
)

// NonceSize is the size of the nonce.
const NonceSize = 32

// Payload represents the entity that forms the Tangle by referencing other Payloads using their trunk and branch.
// A Payload contains a transaction and defines, where in the Tangle a transaction is attached.
type Payload struct {
	objectstorage.StorableObjectFlags

	id      *ID
	idMutex sync.RWMutex

	nonce      []byte
	like       uint32
	bytes      []byte
	bytesMutex sync.RWMutex
}

// New is the constructor of a Payload and creates a new Payload object from the given details.
func New(like uint32, nonce []byte) *Payload {
	if len(nonce) < NonceSize {
		return nil
	}
	p := &Payload{
		like:  like,
		nonce: make([]byte, NonceSize),
	}
	copy(p.nonce, nonce[:NonceSize])
	return p
}

// FromBytes parses the marshaled version of a Payload into an object.
// It either returns a new Payload or fills an optionally provided Payload with the parsed information.
func FromBytes(bytes []byte, optionalTargetObject ...*Payload) (result *Payload, consumedBytes int, err error) {
	marshalUtil := marshalutil.New(bytes)
	result, err = Parse(marshalUtil, optionalTargetObject...)
	consumedBytes = marshalUtil.ReadOffset()

	return
}

// FromStorageKey is a factory method that creates a new Payload instance from a storage key of the objectstorage.
// It is used by the objectstorage, to create new instances of this entity.
func FromStorageKey(key []byte, optionalTargetObject ...*Payload) (result *Payload, consumedBytes int, err error) {
	// determine the target object that will hold the unmarshaled information
	switch len(optionalTargetObject) {
	case 0:
		result = &Payload{}
	case 1:
		result = optionalTargetObject[0]
	default:
		panic("too many arguments in call to MissingPayloadFromStorageKey")
	}

	// parse the properties that are stored in the key
	marshalUtil := marshalutil.New(key)
	payloadID, idErr := ParseID(marshalUtil)
	if idErr != nil {
		err = idErr

		return
	}
	result.id = &payloadID
	consumedBytes = marshalUtil.ReadOffset()

	return
}

// Parse unmarshals a Payload using the given marshalUtil (for easier marshaling/unmarshaling).
func Parse(marshalUtil *marshalutil.MarshalUtil, optionalTargetObject ...*Payload) (result *Payload, err error) {
	// determine the target object that will hold the unmarshaled information
	switch len(optionalTargetObject) {
	case 0:
		result = &Payload{}
	case 1:
		result = optionalTargetObject[0]
	default:
		panic("too many arguments in call to Parse")
	}

	_, err = marshalUtil.Parse(func(data []byte) (parseResult interface{}, parsedBytes int, parseErr error) {
		parsedBytes, parseErr = result.UnmarshalObjectStorageValue(data)

		return
	})

	return
}

// ID returns the identifier if the Payload.
func (payload *Payload) ID() ID {
	// acquire lock for reading id
	payload.idMutex.RLock()

	// return if id has been calculated already
	if payload.id != nil {
		defer payload.idMutex.RUnlock()

		return *payload.id
	}

	// switch to write lock
	payload.idMutex.RUnlock()
	payload.idMutex.Lock()
	defer payload.idMutex.Unlock()

	// return if id has been calculated in the mean time
	if payload.id != nil {
		return *payload.id
	}

	// otherwise calculate the id
	marshalUtil := marshalutil.New(NonceSize + marshalutil.UINT32_SIZE)
	marshalUtil.WriteBytes(payload.Nonce())
	marshalUtil.WriteUint32(payload.Like())

	var id ID = blake2b.Sum256(marshalUtil.Bytes())
	payload.id = &id

	return id
}

// Nonce returns the nonce of the Payload.
func (payload *Payload) Nonce() []byte {
	return payload.nonce
}

// Like returns the like of this Payload.
func (payload *Payload) Like() uint32 {
	return payload.like
}

// Bytes returns a marshaled version of this Payload.
func (payload *Payload) Bytes() []byte {
	return payload.ObjectStorageValue()
}

func (payload *Payload) String() string {
	return stringify.Struct("Payload",
		stringify.StructField("ID", payload.ID()),
		stringify.StructField("nonce", payload.Nonce()),
		stringify.StructField("like", payload.Like()),
	)
}

// region Payload implementation ///////////////////////////////////////////////////////////////////////////////////////

// Type represents the identifier which addresses the value Payload type.
const Type = payload.Type(10895)

// Type returns the type of the Payload.
func (payload *Payload) Type() payload.Type {
	return Type
}

// ObjectStorageValue returns the bytes that represent all remaining information (not stored in the key) of a marshaled
// Branch.
func (payload *Payload) ObjectStorageValue() (bytes []byte) {
	// acquire lock for reading bytes
	payload.bytesMutex.RLock()

	// return if bytes have been determined already
	if bytes = payload.bytes; bytes != nil {
		defer payload.bytesMutex.RUnlock()

		return
	}

	// switch to write lock
	payload.bytesMutex.RUnlock()
	payload.bytesMutex.Lock()
	defer payload.bytesMutex.Unlock()

	// return if bytes have been determined in the mean time
	if bytes = payload.bytes; bytes != nil {
		return
	}

	// marshal fields
	payloadLength := NonceSize + marshalutil.UINT32_SIZE
	marshalUtil := marshalutil.New(marshalutil.UINT32_SIZE + marshalutil.UINT32_SIZE + payloadLength)
	marshalUtil.WriteUint32(Type)
	marshalUtil.WriteUint32(uint32(payloadLength))
	marshalUtil.WriteBytes(payload.Nonce())
	marshalUtil.WriteUint32(payload.Like())
	bytes = marshalUtil.Bytes()

	// store result
	payload.bytes = bytes

	return
}

// UnmarshalObjectStorageValue unmarshals the bytes that are stored in the value of the objectstorage.
func (payload *Payload) UnmarshalObjectStorageValue(data []byte) (consumedBytes int, err error) {
	marshalUtil := marshalutil.New(data)

	// read information that are required to identify the payload from the outside
	_, err = marshalUtil.ReadUint32()
	if err != nil {
		return
	}
	_, err = marshalUtil.ReadUint32()
	if err != nil {
		return
	}

	// parse payload
	if payload.nonce, err = marshalUtil.ReadBytes(NonceSize); err != nil {
		return
	}
	if payload.like, err = marshalUtil.ReadUint32(); err != nil {
		return
	}

	// return the number of bytes we processed
	consumedBytes = marshalUtil.ReadOffset()

	// store bytes, so we don't have to marshal manually
	payload.bytes = make([]byte, consumedBytes)
	copy(payload.bytes, data[:consumedBytes])

	return
}

// Unmarshal unmarshals a given slice of bytes and fills the object with the.
func (payload *Payload) Unmarshal(data []byte) (err error) {
	_, _, err = FromBytes(data, payload)

	return
}

func init() {
	payload.RegisterType(Type, func(data []byte) (payload payload.Payload, err error) {
		payload, _, err = FromBytes(data)

		return
	})
}

// define contract (ensure that the struct fulfills the corresponding interface)
var _ payload.Payload = &Payload{}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region StorableObject implementation ////////////////////////////////////////////////////////////////////////////////

// ObjectStorageKey returns the bytes that are used a key when storing the Branch in an objectstorage.
func (payload *Payload) ObjectStorageKey() []byte {
	return payload.ID().Bytes()
}

// Update is disabled but needs to be implemented to be compatible with the objectstorage.
func (payload *Payload) Update(other objectstorage.StorableObject) {
	panic("a Payload should never be updated")
}

// define contract (ensure that the struct fulfills the corresponding interface)
var _ objectstorage.StorableObject = &Payload{}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// CachedPayload is a wrapper for the object storage, that takes care of type casting the managed objects.
// Since go does not have generics (yet), the object storage works based on the generic "interface{}" type, which means
// that we have to regularly type cast the returned objects, to match the expected type. To reduce the burden of
// manually managing these type, we create a wrapper that does this for us. This way, we can consistently handle the
// specialized types of CachedObjects, without having to manually type cast over and over again.
type CachedPayload struct {
	objectstorage.CachedObject
}

// Retain wraps the underlying method to return a new "wrapped object".
func (cachedPayload *CachedPayload) Retain() *CachedPayload {
	return &CachedPayload{cachedPayload.CachedObject.Retain()}
}

// Consume wraps the underlying method to return the correctly typed objects in the callback.
func (cachedPayload *CachedPayload) Consume(consumer func(payload *Payload)) bool {
	return cachedPayload.CachedObject.Consume(func(object objectstorage.StorableObject) {
		consumer(object.(*Payload))
	})
}

// Unwrap provides a way to "Get" a type casted version of the underlying object.
func (cachedPayload *CachedPayload) Unwrap() *Payload {
	untypedTransaction := cachedPayload.Get()
	if untypedTransaction == nil {
		return nil
	}

	typeCastedTransaction := untypedTransaction.(*Payload)
	if typeCastedTransaction == nil || typeCastedTransaction.IsDeleted() {
		return nil
	}

	return typeCastedTransaction
}