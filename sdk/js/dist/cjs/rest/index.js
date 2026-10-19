"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.Querier = void 0;
const query_1 = require("./cosmos/accesscontrol_x/query");
const query_2 = require("./cosmos/auth/v1beta1/query");
const query_3 = require("./cosmos/authz/v1beta1/query");
const query_4 = require("./cosmos/bank/v1beta1/query");
const query_5 = require("./cosmos/distribution/v1beta1/query");
const query_6 = require("./cosmos/evidence/v1beta1/query");
const query_7 = require("./cosmos/feegrant/v1beta1/query");
const query_8 = require("./cosmos/gov/v1beta1/query");
const query_9 = require("./cosmos/mint/v1beta1/query");
const query_10 = require("./cosmos/params/v1beta1/query");
const query_11 = require("./cosmos/slashing/v1beta1/query");
const query_12 = require("./cosmos/staking/v1beta1/query");
const query_13 = require("./cosmos/upgrade/v1beta1/query");
const query_14 = require("./epoch/query");
const query_15 = require("./evm/query");
const query_16 = require("./mint/v1beta1/query");
const query_17 = require("./oracle/query");
const query_18 = require("./tokenfactory/query");
exports.Querier = {
    cosmos: {
        accesscontrol_x: query_1.Query,
        auth: { v1beta1: query_2.Query },
        authz: { v1beta1: query_3.Query },
        bank: { v1beta1: query_4.Query },
        distribution: { v1beta1: query_5.Query },
        evidence: { v1beta1: query_6.Query },
        feegrant: { v1beta1: query_7.Query },
        gov: { v1beta1: query_8.Query },
        mint: { v1beta1: query_9.Query },
        params: { v1beta1: query_10.Query },
        slashing: { v1beta1: query_11.Query },
        staking: { v1beta1: query_12.Query },
        upgrade: { v1beta1: query_13.Query }
    },
    epoch: query_14.Query,
    evm: query_15.Query,
    mint: { v1beta1: query_16.Query },
    oracle: query_17.Query,
    tokenfactory: query_18.Query
};
