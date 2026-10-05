---
order: 1
parent:
  title: P2P
  order: 6
---

# P2P

Specification of the peer-to-peer layer. The implementation in this tree is [`internal/p2p`](../../internal/p2p/).

- [Node](./node.md) - node types and how they discover and connect to peers
- [Peer](./peer.md) - peer identity, authentication and handshakes
- [Connection](./connection.md) - multiplexed connections and channels
- [Config](./config.md) - p2p configuration options
- [Messages](./messages/README.md) - the messages of each reactor
