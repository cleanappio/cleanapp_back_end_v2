"use strict";

import { PermissionsAndroid } from 'react-native';
import { isAndroid } from './utils';
export async function requestAndroidLocationPermissions() {
  if (isAndroid()) {
    const res = await PermissionsAndroid.requestMultiple([PermissionsAndroid.PERMISSIONS.ACCESS_FINE_LOCATION, PermissionsAndroid.PERMISSIONS.ACCESS_COARSE_LOCATION]);
    if (!res) {
      return false;
    }
    const permissions = Object.keys(res);
    for (const permission of permissions) {
      if (res[permission] === PermissionsAndroid.RESULTS.GRANTED) {
        return true;
      }
    }
    return false;
  }
  throw new Error('You should only call this method on Android!');
}
//# sourceMappingURL=requestAndroidLocationPermissions.js.map