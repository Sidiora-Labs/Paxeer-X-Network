# Pax custom backends that implement Tendermint DB interface
## Arweave
Arweave is a decentralized, durable data storage solution. To store data in Arweave,
one would need to send it as a transaction. Once the transaction is accepted and processed
by Arweave, one can query that data by the transaction ID, which is a hash of the transaction
signature. As such, from a usability perspective, the biggest difference between Arweave and
a traditional KV store is that one cannot pick his own key, and consequently an index data
structure is needed to map a key to the transactions that *may* contain it.
### Design
Since it's not economic to send one transaction per key-value pair, we will batch store multiple
key-value pairs in one transaction. The key-to-transaction index will therefore be a
key-range-to-transaction index. To make the index size more compact, we will require the key range
to be truncated (or padded) as fix-sized bytes, which means it's possible to have multiple transactions
for the same (truncated) key range, so all of such transactions need to be read at query time to find
where the exact key resides. Each version will have its own index, and each index version is stored
as individual transaction on Arweave. The transaction IDs for each index version are stored on a
local leveldb; which is the only data required to be stored locally.
