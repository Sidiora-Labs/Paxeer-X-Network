package engine

import (
 "encoding/json"
 "testing"
 "time"
 "github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
 interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

func TestCanonicalCandleRetainsQualifiedSourceAndEntityRevision(t *testing.T){
 first:=seriesSource(1,1700000000,"");last:=seriesSource(2,1700000001,"")
 bar,err:=FoldQualified(nil,last,"1m",false);if err!=nil{t.Fatal(err)};bar,err=FoldQualified(&bar,first,"1m",false);if err!=nil{t.Fatal(err)}
 state,err:=DescribeSeries(bar,SeriesCheckpoint{Height:500,Timestamp:1700000600},time.Unix(1700000601,0));if err!=nil{t.Fatal(err)};state.DisplayRevision=19
 sources:=map[string]accounting.FillV1{first.SourceFillID:first,last.SourceFillID:last}
 mapped,err:=CanonicalCandle(state,sources);if err!=nil{t.Fatal(err)}
 if mapped.Revision!="2"||mapped.DisplayRevision!="19"||mapped.Trades!="2"||mapped.Coverage!="complete"||mapped.UnknownActorTradeCount!="2"||mapped.UniqueTraderCount==nil||*mapped.UniqueTraderCount!="0"{t.Fatal(mapped)}
 if mapped.Open.Source.SourceEventId==nil||*mapped.Open.Source.SourceEventId!=first.SourceFillID||mapped.Close.Source.SourceEventId==nil||*mapped.Close.Source.SourceEventId!=last.SourceFillID||mapped.Open.Source.ObservedAt==mapped.Close.Source.ObservedAt||mapped.FirstPosition.BlockHash!=first.Position.BlockHash||mapped.LastPosition.TransactionHash!=last.Position.TransactionHash{t.Fatal(mapped)}
 raw,err:=json.Marshal(mapped);if err!=nil{t.Fatal(err)};if err=interfaces.ValidateJSON("Candle",raw);err!=nil{t.Fatal(err)}
 missing:=map[string]accounting.FillV1{first.SourceFillID:first};if _,err=CanonicalCandle(state,missing);err==nil{t.Fatal("missing authentic close source accepted")}
 changed:=last;changed.BlockTimestampSeconds="1700000002";changed.SourceFillID=first.SourceFillID;sources[last.SourceFillID]=changed;if _,err=CanonicalCandle(state,sources);err==nil{t.Fatal("different source identity accepted")}
}
func TestCanonicalCandlePartialAndSparseContract(t *testing.T){
 source:=seriesSource(1,1700000000,"");bar,err:=FoldQualified(nil,source,"1m",false);if err!=nil{t.Fatal(err)};state,err:=DescribeSeries(bar,SeriesCheckpoint{HasGaps:true},time.Unix(1700000601,0));if err!=nil{t.Fatal(err)};state.DisplayRevision=1
 sources:=map[string]accounting.FillV1{source.SourceFillID:source};mapped,err:=CanonicalCandle(state,sources);if err!=nil||mapped.Coverage!="partial"||mapped.SourceWatermarkHeight!="0"{t.Fatal(mapped,err)}
 state.Synthetic=true;if _,err=CanonicalCandle(state,sources);err==nil{t.Fatal("synthetic activity published")}
 state.Synthetic=false;state.Trades=0;if _,err=CanonicalCandle(state,sources);err==nil{t.Fatal("zero-trade canonical bar published")}
}
