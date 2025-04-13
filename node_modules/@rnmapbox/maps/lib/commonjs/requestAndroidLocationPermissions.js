"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.requestAndroidLocationPermissions = requestAndroidLocationPermissions;
var _reactNative = require("react-native");
var _utils = require("./utils");
async function requestAndroidLocationPermissions() {
  if ((0, _utils.isAndroid)()) {
    const res = await _reactNative.PermissionsAndroid.requestMultiple([_reactNative.PermissionsAndroid.PERMISSIONS.ACCESS_FINE_LOCATION, _reactNative.PermissionsAndroid.PERMISSIONS.ACCESS_COARSE_LOCATION]);
    if (!res) {
      return false;
    }
    const permissions = Object.keys(res);
    for (const permission of permissions) {
      if (res[permission] === _reactNative.PermissionsAndroid.RESULTS.GRANTED) {
        return true;
      }
    }
    return false;
  }
  throw new Error('You should only call this method on Android!');
}
//# sourceMappingURL=requestAndroidLocationPermissions.js.map