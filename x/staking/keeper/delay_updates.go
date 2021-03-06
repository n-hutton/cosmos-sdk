package keeper

import (
	"fmt"

	abci "github.com/tendermint/tendermint/abci/types"
	tmtypes "github.com/tendermint/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	computeValidatorUpdateKey    = []byte("computeValidatorUpdateKey")
	computeDKGValidatorUpdateKey = []byte("computeDKGValidatorUpdateKey")
	validatorUpdatesKey          = []byte("validatorUpdatesKey")
	jailedValidatorUpdatesKey    = []byte("jailedValidatorUpdatesKey")
)

// CheckValidatorUpdates determines whether block height is sufficiently close to the next aeon start
// to trigger dkg and consensus validator changes
func (k Keeper) CheckValidatorUpdates(ctx sdk.Context, header abci.Header) {
	// One block before a new aeon start need to compute validator updates for next dkg committee.
	// Two blocks before a new aeon start need to update consensus committee to those which ran dkg
	nextAeonStart := header.Entropy.NextAeonStart
	if !k.delayValidatorUpdates || header.Height == nextAeonStart-1 {
		store := ctx.KVStore(k.storeKey)
		store.Set(computeDKGValidatorUpdateKey, []byte{0})
	}
	if !k.delayValidatorUpdates || header.Height == nextAeonStart-2 {
		store := ctx.KVStore(k.storeKey)
		store.Set(computeValidatorUpdateKey, []byte{0})
	}
}

// DKGValidatorUpdates returns dkg validator updates to EndBlock at block height set by BeginBlock and
// saves them to store for retrieval by ValidatorUpdates
func (k Keeper) DKGValidatorUpdates(ctx sdk.Context) []abci.ValidatorUpdate {
	store := ctx.KVStore(k.storeKey)
	if len(store.Get(computeDKGValidatorUpdateKey)) == 0 {
		return []abci.ValidatorUpdate{}
	}
	store.Set(computeDKGValidatorUpdateKey, []byte{})
	// Calculate validator set changes.
	//
	// NOTE: ApplyAndReturnValidatorSetUpdates has to come before
	// UnbondAllMatureValidatorQueue.
	// This fixes a bug when the unbonding period is instant (is the case in
	// some of the tests). The test expected the validator to be completely
	// unbonded after the Endblocker (go from Bonded -> Unbonding during
	// ApplyAndReturnValidatorSetUpdates and then Unbonding -> Unbonded during
	// UnbondAllMatureValidatorQueue).
	updates := k.ApplyAndReturnValidatorSetUpdates(ctx)
	k.setDKGValidatorUpdates(ctx, updates)
	return updates
}

// ValidatorUpdates retrieve last saved updates from store when non-trivial update
// is triggered by BeginBlock,
func (k Keeper) ValidatorUpdates(ctx sdk.Context) []abci.ValidatorUpdate {
	// Away from validator changeover points we only remove jailed validators from the consensus
	// validator set
	consensusUpdates := k.checkJailedValidatorUpdates(ctx)
	if len(k.getComputeValUpdateKey(ctx)) != 0 {
		dkgUpdates := k.getDKGValidatorUpdates(ctx)
		consensusUpdates = k.ConsensusFromDKGUpdates(ctx, dkgUpdates)
	}
	k.RemoveMatureQueueItems(ctx)
	return consensusUpdates
}

func (k Keeper) getComputeValUpdateKey(ctx sdk.Context) []byte {
	store := ctx.KVStore(k.storeKey)
	return store.Get(computeValidatorUpdateKey)
}

func (k Keeper) getDKGValidatorUpdates(ctx sdk.Context) []abci.ValidatorUpdate {
	store := ctx.KVStore(k.storeKey)
	updateBytes := store.Get(validatorUpdatesKey)
	updates := []abci.ValidatorUpdate{}
	k.cdc.UnmarshalBinaryLengthPrefixed(updateBytes, &updates)
	store.Set(computeValidatorUpdateKey, []byte{})
	return updates
}

func (k Keeper) setDKGValidatorUpdates(ctx sdk.Context, update []abci.ValidatorUpdate) {
	store := ctx.KVStore(k.storeKey)
	store.Set(validatorUpdatesKey, k.cdc.MustMarshalBinaryLengthPrefixed(update))
}

// Get validators which have been jailed since last block and then reset the stored updates
func (k Keeper) checkJailedValidatorUpdates(ctx sdk.Context) []abci.ValidatorUpdate {
	updates := k.getJailedValidatorUpdates(ctx)
	k.setJailedValidatorUpdates(ctx, []abci.ValidatorUpdate{})
	// Turn off producing blocks for jailed validators
	for _, val := range updates {
		pubKey, err := tmtypes.PB2TM.PubKey(val.PubKey)
		if err != nil {
			panic(fmt.Sprintf("Error converting public key %v in validator updates", val.PubKey))
		}
		validator := k.mustGetValidatorByConsAddr(ctx, sdk.GetConsAddress(pubKey))
		k.stopProducingBlocks(ctx, validator)
	}

	return updates
}

func (k Keeper) getJailedValidatorUpdates(ctx sdk.Context) []abci.ValidatorUpdate {
	store := ctx.KVStore(k.storeKey)
	updateBytes := store.Get(jailedValidatorUpdatesKey)
	updates := []abci.ValidatorUpdate{}
	k.cdc.UnmarshalBinaryLengthPrefixed(updateBytes, &updates)
	return updates
}

func (k Keeper) setJailedValidatorUpdates(ctx sdk.Context, updates []abci.ValidatorUpdate) {
	store := ctx.KVStore(k.storeKey)
	store.Set(jailedValidatorUpdatesKey, k.cdc.MustMarshalBinaryLengthPrefixed(updates))
}
