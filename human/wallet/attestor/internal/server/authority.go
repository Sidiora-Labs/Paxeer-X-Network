package server

import (
    "bytes"
    "encoding/hex"
    "encoding/json"
    "io"
    "net/http"
    "os"
    "sort"
    "strings"
    "sync"
    "time"

    "github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/auth/agent"
    "github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/store"
    "github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/dealer"
)

const PathAuthority = "/v1/authority"

type InventoryMember struct { ID string `json:"id"`; Pin string `json:"spki_sha256"` }
type InventoryKey struct {
    KeyID string `json:"key_id"`
    Epoch uint64 `json:"epoch"`
    Curve string `json:"curve"`
    PublicKey string `json:"public_key"`
    Owner string `json:"owner"`
    Operations []string `json:"operations"`
}
type WalletInventory struct {
    agent.SignedClaims
    Protocol string `json:"protocol"`
    Threshold int `json:"threshold"`
    Members []InventoryMember `json:"members"`
    Keys []InventoryKey `json:"keys"`
}
type Inventory struct {
    mu sync.Mutex
    path string
    verifier *agent.SignedVerifier
    store *store.Store
    pins map[string]string
}

func NewInventory(path,publicKeyFile,issuer,tenant string,st *store.Store,pins map[string]string)(*Inventory,error){
    if path==""||st==nil||len(pins)!=5{return nil,agent.ErrAuthority}
    verifier,err:=agent.NewSignedVerifier(publicKeyFile,issuer,tenant);if err!=nil{return nil,err}
    i:=&Inventory{path:path,verifier:verifier,store:st,pins:pins}
    if _,err=i.load();err!=nil{return nil,err};return i,nil
}
func (i *Inventory) load()(WalletInventory,error){
    i.mu.Lock();defer i.mu.Unlock()
    var inventory WalletInventory
    f,err:=os.Open(i.path);if err!=nil{return inventory,agent.ErrAuthority};defer f.Close()
    raw,err:=io.ReadAll(io.LimitReader(f,(2<<20)+1));if err!=nil||len(raw)>2<<20{return inventory,agent.ErrAuthority}
    token:=strings.TrimSpace(string(raw))
    if err=i.verifier.Decode(token,agent.InventoryAudience,30*24*time.Hour,&inventory);err!=nil{return inventory,err}
    if inventory.Protocol!="wallet"||inventory.Threshold!=3||len(inventory.Members)!=5||len(inventory.Keys)>65536{return inventory,agent.ErrAuthority}
    seen:=map[string]bool{};seenPins:=map[string]bool{}
    for _,member:=range inventory.Members{
        if member.ID==""||seen[member.ID]||seenPins[member.Pin]||i.pins[member.ID]!=member.Pin{return inventory,agent.ErrAuthority}
        pin,e:=hex.DecodeString(member.Pin);if e!=nil||len(pin)!=32{return inventory,agent.ErrAuthority};seen[member.ID]=true;seenPins[member.Pin]=true
    }
    seen=map[string]bool{}
    for _,key:=range inventory.Keys{
        if key.KeyID==""||key.Owner==""||seen[key.KeyID]||(key.Curve!="secp256k1"&&key.Curve!="ed25519")||len(key.Operations)==0{return inventory,agent.ErrAuthority};seen[key.KeyID]=true
        ops:=map[string]bool{}
        for _,op:=range key.Operations{if ops[op]||(op!="sign"&&op!="generate"&&op!="import"&&op!="refresh"){return inventory,agent.ErrAuthority};ops[op]=true}
        if key.PublicKey=="" {if !ops["generate"]||len(ops)!=1||key.Epoch!=0{return inventory,agent.ErrAuthority}} else {
            pub,e:=hex.DecodeString(key.PublicKey);if e!=nil||key.PublicKey!=strings.ToLower(key.PublicKey)||(key.Curve=="ed25519"&&len(pub)!=32)||(key.Curve=="secp256k1"&&(len(pub)!=65||pub[0]!=4)){return inventory,agent.ErrAuthority}
        }
    }
    if err=agent.PersistSigned(i.store,agent.InventoryAudience,inventory.Sequence,token);err!=nil{return inventory,err}
    return inventory,nil
}
func (i *Inventory) Check(keyID,operation,owner,curve string,record *store.ShareRecord)error{
    if i==nil{return agent.ErrAuthority};inventory,err:=i.load();if err!=nil{return err}
    var approved *InventoryKey
    for n:=range inventory.Keys{if inventory.Keys[n].KeyID==keyID{approved=&inventory.Keys[n];break}}
    if approved==nil||approved.Owner!=owner||approved.Curve!=curve{return agent.ErrAuthority}
    allowed:=false;for _,op:=range approved.Operations{allowed=allowed||op==operation};if !allowed{return agent.ErrAuthority}
    if record!=nil {
        if record.Epoch!=approved.Epoch||hex.EncodeToString(record.PublicKey)!=approved.PublicKey{return agent.ErrAuthority}
        held:=append([]string(nil),record.Participants...);want:=make([]string,0,5);for _,m:=range inventory.Members{want=append(want,m.ID)};sort.Strings(held);sort.Strings(want)
        if len(held)!=5||strings.Join(held,"\x00")!=strings.Join(want,"\x00"){return agent.ErrAuthority}
    } else if operation!="generate"||approved.PublicKey!=""||approved.Epoch!=0{return agent.ErrAuthority}
    return nil
}

func (s *Server) HandleAuthority(w http.ResponseWriter,r *http.Request){
    body,e:=readBody(r)
    if e!=nil{writeError(w,e);return}
    var request struct { Token string `json:"token"` }
    if e=decodeRequest(body,&request);e!=nil{writeError(w,e);return}
    if s.opts.Authority==nil{writeError(w,newError(CodeTokenUnavailable,"custody authority is not configured"));return}
    sequence,err:=s.opts.Authority.Admit(request.Token)
    if err!=nil{writeError(w,newError(CodeAgentInvalid,"custody authority snapshot refused"));return}
    if _,e=s.audit("custody.authority","", "producer","allowed",sequence,"");e!=nil{writeError(w,e);return}
    writeJSON(w,http.StatusOK,map[string]string{"node_id":s.opts.NodeID,"sequence":sequence})
}

func (s *Server) inventoryRoute(operation string,next http.HandlerFunc)http.HandlerFunc{
    return func(w http.ResponseWriter,r *http.Request){
        if operation=="import"||operation=="refresh" {next(w,r);return}
        body,e:=readBody(r);if e!=nil{writeError(w,e);return}
        var request struct { KeyID string `json:"key_id"`; Owner string `json:"owner"`; Curve string `json:"curve"` }
        if json.Unmarshal(body,&request)!=nil{writeError(w,newError(CodeSessionBadRequest,"invalid key operation"));return}
        owner,curve:=request.Owner,request.Curve
        var rec *store.ShareRecord
        if operation=="import" {
            var imported ImportRequest
            if e:=decodeRequest(body,&imported);e!=nil{writeError(w,e);return}
            bundle,err:=DecodeBundle(imported.Share)
            if err!=nil{writeError(w,newError(CodeKeyInvalidShare,"invalid imported share"));return}
            defer dealer.Wipe(bundle.Share)
            if e:=s.checkImportedScheme(bundle);e!=nil{writeError(w,e);return}
            pub,err:=publicKeyBytes(bundle.Curve,bundle.PublicKey)
            if err!=nil{writeError(w,newError(CodeKeyInvalidShare,"invalid imported public key"));return}
            rec=&store.ShareRecord{KeyID:imported.KeyID,Curve:curveName(bundle.Curve),PublicKey:pub,Epoch:0,Participants:bundle.ParticipantIDs()}
            owner=imported.Owner;curve=rec.Curve
        } else if operation!="generate"{
            record,payload,err:=s.loadShare(request.KeyID)
            if err!=nil{writeError(w,err);return};rec=&record;owner=payload.Owner;curve=record.Curve
        }
        if err:=s.opts.Inventory.Check(request.KeyID,operation,owner,curve,rec);err!=nil{
            writeError(w,s.deny("keys."+operation,request.KeyID,owner,"denied","",newError(CodeKeyInvalidShare,"owner-approved wallet inventory does not admit operation")));return
        }
        if operation=="generate" {
            record,payload,err:=s.loadShare(request.KeyID)
            if err==nil {
                if record.Epoch!=0||payload.Owner!=owner||record.Curve!=curve||len(record.Participants)!=5 {
                    writeError(w,newError(CodeKeyInvalidShare,"existing generation differs from approved request"));return
                }
                inventory,loadErr:=s.opts.Inventory.load();if loadErr!=nil{writeError(w,newError(CodeKeyInvalidShare,"inventory unavailable"));return}
                known:=map[string]bool{};for _,member:=range inventory.Members{known[member.ID]=true};for _,id:=range record.Participants{if !known[id]{writeError(w,newError(CodeKeyInvalidShare,"existing generation membership differs"));return};delete(known,id)}
                if len(known)!=0{writeError(w,newError(CodeKeyInvalidShare,"existing generation membership differs"));return}
                sequence,e:=s.audit("keys.generate",request.KeyID,owner,"allowed","recovered durable generation","");if e!=nil{writeError(w,e);return}
                response:=KeyResponse{NodeID:s.opts.NodeID,KeyID:record.KeyID,Curve:record.Curve,PublicKey:hex.EncodeToString(record.PublicKey),Epoch:record.Epoch,Participants:record.Participants,Refreshed:len(payload.ECDSA)>0,AuditSequence:sequence}
                if curve=="secp256k1"{response.Address=payload.Account}else{response.DID="did:layerx:"+hex.EncodeToString(record.PublicKey)}
                writeJSON(w,http.StatusOK,response);return
            }
            if err.Code!=CodeKeyNotFound{writeError(w,err);return}
        }
        r.Body=io.NopCloser(bytes.NewReader(body));next(w,r)
    }
}

