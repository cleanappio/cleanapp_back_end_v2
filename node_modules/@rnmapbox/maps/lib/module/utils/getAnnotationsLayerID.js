"use strict";

import { Platform } from 'react-native';

/*
 * Retrieve the layer ids used for PointAnnotations and Callouts
 */
export const getAnnotationsLayerID = type => {
  return Platform.select({
    android: 'RNMBX-mapview-annotations',
    ios: {
      PointAnnotations: 'RNMBX-mapview-point-annotations',
      Callouts: 'RNMBX-mapview-callouts'
    }[type]
  });
};
//# sourceMappingURL=getAnnotationsLayerID.js.map