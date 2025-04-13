"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.setWellKnownTileServer = exports.setTelemetryEnabled = exports.setConnected = exports.setAccessToken = exports.removeCustomHeader = exports.getAccessToken = exports.clearData = exports.addCustomHeader = exports.TileServers = exports.StyleURL = exports.StyleSource = exports.OfflinePackDownloadState = exports.LineJoin = void 0;
var _reactNative = require("react-native");
// eslint-disable-next-line prefer-destructuring
const RNMBXModule = _reactNative.NativeModules.RNMBXModule;
if (_reactNative.NativeModules.RNMBXModule == null) {
  if (global.expo != null) {
    // global.expo.modules.ExponentConstants;
    throw new Error('@rnmapbox/maps native code not available. Make sure you have linked the library and rebuild your app. See https://rnmapbox.github.io/docs/install?rebuild=expo#rebuild');
  } else {
    throw new Error('@rnmapbox/maps native code not available. Make sure you have linked the library and rebuild your app. See https://rnmapbox.github.io/docs/install');
  }
}
const {
  StyleURL,
  OfflinePackDownloadState,
  LineJoin,
  StyleSource,
  TileServers,
  removeCustomHeader,
  addCustomHeader,
  setAccessToken,
  setWellKnownTileServer,
  clearData,
  getAccessToken,
  setTelemetryEnabled,
  setConnected
} = RNMBXModule;
exports.setConnected = setConnected;
exports.setTelemetryEnabled = setTelemetryEnabled;
exports.getAccessToken = getAccessToken;
exports.clearData = clearData;
exports.setWellKnownTileServer = setWellKnownTileServer;
exports.setAccessToken = setAccessToken;
exports.addCustomHeader = addCustomHeader;
exports.removeCustomHeader = removeCustomHeader;
exports.TileServers = TileServers;
exports.StyleSource = StyleSource;
exports.LineJoin = LineJoin;
exports.OfflinePackDownloadState = OfflinePackDownloadState;
exports.StyleURL = StyleURL;
//# sourceMappingURL=RNMBXModule.js.map