package export

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/pubsub/query"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/store"
)

type Query = query.Query

var NewBlockStore = store.NewBlockStore
var NewStore = state.NewStore
var NewQuery = query.New
var QueryAll = query.All
