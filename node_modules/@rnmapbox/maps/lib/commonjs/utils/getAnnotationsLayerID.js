"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.getAnnotationsLayerID = void 0;
var _reactNative = require("react-native");
/*
 * Retrieve the layer ids used for PointAnnotations and Callouts
 */
const getAnnotationsLayerID = type => {
  return _reactNative.Platform.select({
    android: 'RNMBX-mapview-annotations',
    ios: {
      PointAnnotations: 'RNMBX-mapview-point-annotations',
      Callouts: 'RNMBX-mapview-callouts'
    }[type]
  });
};
exports.getAnnotationsLayerID = getAnnotationsLayerID;
//# sourceMappingURL=getAnnotationsLayerID.js.map