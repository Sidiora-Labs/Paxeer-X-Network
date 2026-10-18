const root=process.env.CONTRACT_SOURCE_ROOT||__dirname+'/../../../../../../../contracts';
module.exports={networks:{lane:{url:'http://127.0.0.1:22700',chainId:125}},paths:{cache:__dirname+'/.private-cache',artifacts:root+'/artifacts'}};
