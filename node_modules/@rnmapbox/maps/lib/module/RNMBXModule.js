"use strict";

import { NativeModules } from 'react-native';
// eslint-disable-next-line prefer-destructuring
const RNMBXModule = NativeModules.RNMBXModule;
if (NativeModules.RNMBXModule == null) {
  if (global.expo != null) {
    // global.expo.modules.ExponentConstants;
    throw new Error('@rnmapbox/maps native code not available. Make sure you have linked the library and rebuild your app. See https://rnmapbox.github.io/docs/install?rebuild=expo#rebuild');
  } else {
    throw new Error('@rnmapbox/maps native code not available. Make sure you have linked the library and rebuild your app. See https://rnmapbox.github.io/docs/install');
  }
}
export const {
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
//# sourceMappingURL=RNMBXModule.js.map