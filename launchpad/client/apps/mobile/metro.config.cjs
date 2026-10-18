const {getDefaultConfig, mergeConfig} = require('@react-native/metro-config');
const path = require('path');
const fs = require('fs');
const packages = path.resolve(__dirname, '../../packages');
module.exports = mergeConfig(getDefaultConfig(__dirname), {
 watchFolders: [packages],
 resolver: {
  resolveRequest(context, name, platform) {
   if (name.startsWith('.') && name.endsWith('.js') && context.originModulePath.startsWith(packages + path.sep)) {
    const source = path.resolve(path.dirname(context.originModulePath), name.slice(0, -3) + '.ts');
    if (fs.existsSync(source)) return {type: 'sourceFile', filePath: source};
   }
   return context.resolveRequest(context, name, platform);
  },
 },
});
