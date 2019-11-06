package ledgerstate

import (
	"encoding/binary"
	"fmt"

	"github.com/iotaledger/goshimmer/packages/errors"

	"github.com/iotaledger/goshimmer/packages/stringify"

	"github.com/iotaledger/goshimmer/packages/objectstorage"
)

type Reality struct {
	id              RealityId
	parentRealities []RealityId

	storageKey  []byte
	ledgerState *LedgerState
}

func newReality(id RealityId, parentRealities ...RealityId) *Reality {
	result := &Reality{
		id:              id,
		parentRealities: parentRealities,

		storageKey: make([]byte, len(id)),
	}
	copy(result.storageKey, id[:])

	return result
}

func (reality *Reality) GetId() RealityId {
	return reality.id
}

func (reality *Reality) DescendsFromReality(realityId RealityId) bool {
	if reality.id == realityId {
		return true
	} else {
		descendsFromReality := false

		for ancestorRealityId, ancestorReality := range reality.GetAncestorRealities() {
			if ancestorRealityId == realityId {
				descendsFromReality = true
			}

			ancestorReality.Release()
		}

		return descendsFromReality
	}
}

func (reality *Reality) GetParentRealities() map[RealityId]*objectstorage.CachedObject {
	parentRealities := make(map[RealityId]*objectstorage.CachedObject)

	for _, parentRealityId := range reality.parentRealities {
		if loadedParentReality := reality.ledgerState.GetReality(parentRealityId); !loadedParentReality.Exists() {
			panic("could not load parent reality with id \"" + string(parentRealityId[:]) + "\"")
		} else {
			parentRealities[loadedParentReality.Get().(*Reality).GetId()] = loadedParentReality
		}
	}

	return parentRealities
}

func (reality *Reality) GetAncestorRealities() (result map[RealityId]*objectstorage.CachedObject) {
	result = make(map[RealityId]*objectstorage.CachedObject, 1)

	for parentRealityId, parentReality := range reality.GetParentRealities() {
		result[parentRealityId] = parentReality

		for ancestorId, ancestor := range parentReality.Get().(*Reality).GetAncestorRealities() {
			result[ancestorId] = ancestor
		}
	}

	return
}

func (reality *Reality) checkTransferBalances(inputs []*objectstorage.CachedObject, outputs map[AddressHash][]*ColoredBalance) error {
	totalColoredBalances := make(map[Color]uint64)

	for _, cachedInput := range inputs {
		if !cachedInput.Exists() {
			return errors.New("missing input in transfer")
		}

		transferOutput := cachedInput.Get().(*TransferOutput)
		if !reality.DescendsFromReality(transferOutput.GetRealityId()) {
			return errors.New("the referenced funds do not exist in this reality")
		}

		for _, balance := range transferOutput.GetBalances() {
			totalColoredBalances[balance.GetColor()] += balance.GetValue()
		}
	}

	for _, transferOutput := range outputs {
		for _, balance := range transferOutput {
			color := balance.GetColor()

			totalColoredBalances[color] -= balance.GetValue()

			if totalColoredBalances[color] == 0 {
				delete(totalColoredBalances, color)
			}
		}
	}

	// transfer is valid if sum of funds is 0
	if len(totalColoredBalances) != 0 {
		return errors.New("the sum of the balance changes is not 0")
	}

	return nil
}

func (reality *Reality) CreateReality(id RealityId) *objectstorage.CachedObject {
	newReality := newReality(id, reality.id)
	newReality.ledgerState = reality.ledgerState

	return reality.ledgerState.realities.Store(newReality)
}

func (reality *Reality) BookTransfer(transfer *Transfer) error {
	return reality.bookTransfer(transfer.GetHash(), reality.ledgerState.getTransferInputs(transfer), transfer.GetOutputs())
}

func (reality *Reality) consumeInputs(inputs []*objectstorage.CachedObject, transferHash TransferHash, outputs map[AddressHash][]*ColoredBalance) (conflicting bool, err error) {
	for _, input := range inputs {
		consumedTransferOutput := input.Get().(*TransferOutput)

		inputConflicting, consumersToElevate, consumeErr := consumedTransferOutput.addConsumer(transferHash, outputs)
		if consumeErr != nil {
			err = consumeErr

			return
		}

		if inputConflicting {
			for transferHash, consumerToElevate := range consumersToElevate {
				// elevate previous liked transactions
				fmt.Println(transferHash, consumerToElevate)
			}
		}

		conflicting = conflicting || inputConflicting
	}

	return
}

func (reality *Reality) bookTransfer(transferHash TransferHash, inputs []*objectstorage.CachedObject, outputs map[AddressHash][]*ColoredBalance) error {
	if err := reality.checkTransferBalances(inputs, outputs); err != nil {
		return err
	}

	conflicting, err := reality.consumeInputs(inputs, transferHash, outputs)
	if err != nil {
		return err
	}

	if conflicting {
		var targetRealityId RealityId
		copy(targetRealityId[:], transferHash[:])

		cachedTargetReality := reality.CreateReality(targetRealityId)
		cachedTargetReality.Get().(*Reality).bookTransferOutputs(transferHash, outputs)
		cachedTargetReality.Release()
	} else {
		reality.bookTransferOutputs(transferHash, outputs)
	}

	// 5. release objects

	return nil
}

func (reality *Reality) bookTransferOutputs(transferHash TransferHash, transferOutputs map[AddressHash][]*ColoredBalance) {
	for addressHash, coloredBalances := range transferOutputs {
		createdTransferOutput := NewTransferOutput(reality.ledgerState, reality.id, transferHash, addressHash, coloredBalances...)
		createdBooking := newTransferOutputBooking(reality.id, addressHash, false, transferHash)

		reality.ledgerState.storeTransferOutput(createdTransferOutput).Release()
		reality.ledgerState.storeTransferOutputBooking(createdBooking).Release()
	}
}

func (reality *Reality) String() string {
	return stringify.Struct("Reality",
		stringify.StructField("id", reality.id.String()),
		stringify.StructField("parentRealities", reality.parentRealities),
	)
}

// region support object storage ///////////////////////////////////////////////////////////////////////////////////////

func (reality *Reality) GetStorageKey() []byte {
	return reality.storageKey
}

func (reality *Reality) Update(other objectstorage.StorableObject) {
	if otherReality, ok := other.(*Reality); !ok {
		panic("Update method expects a *TransferOutputBooking")
	} else {
		reality.parentRealities = otherReality.parentRealities
	}
}

func (reality *Reality) MarshalBinary() ([]byte, error) {
	parentRealityCount := len(reality.parentRealities)

	marshaledReality := make([]byte, 4+parentRealityCount*realityIdLength)

	binary.LittleEndian.PutUint32(marshaledReality, uint32(parentRealityCount))
	for i := 0; i < parentRealityCount; i++ {
		copy(marshaledReality[4+i*realityIdLength:], reality.parentRealities[i][:])
	}

	return marshaledReality, nil
}

func (reality *Reality) UnmarshalBinary(serializedObject []byte) error {
	if err := reality.id.UnmarshalBinary(reality.storageKey[:realityIdLength]); err != nil {
		return err
	}

	parentRealityCount := int(binary.LittleEndian.Uint32(serializedObject))
	parentRealities := make([]RealityId, parentRealityCount)
	for i := 0; i < parentRealityCount; i++ {
		if err := parentRealities[i].UnmarshalBinary(serializedObject[4+i*realityIdLength:]); err != nil {
			return err
		}
	}

	return nil
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////