"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _reactNative = require("react-native");
var _utils = require("../../utils");
var _geoUtils = require("../../utils/geoUtils");
const MapboxGL = _reactNative.NativeModules.RNMBXModule;
class SnapshotOptions {
  constructor(options = {}) {
    if (!options.centerCoordinate && !options.bounds) {
      throw new Error('Center coordinate or bounds must be supplied in order to take a snapshot');
    }
    this.styleURL = options.styleURL || MapboxGL.StyleURL.Street;
    this.heading = options.heading || 0.0;
    this.pitch = options.pitch || 0.0;
    this.zoomLevel = options.zoomLevel || 16.0;
    this.width = options.width || 50.0;
    this.height = options.height || 50.0;
    this.writeToDisk = options.writeToDisk || false;
    this.withLogo = options.withLogo === undefined ? true : options.withLogo;
    if (options.centerCoordinate) {
      this.centerCoordinate = this._createCenterCoordPoint(options.centerCoordinate);
    }
    if (options.bounds) {
      this.bounds = this._createBoundsCollection(options.bounds);
    }
  }
  toJSON() {
    return {
      styleURL: this.styleURL,
      heading: this.heading,
      pitch: this.pitch,
      zoomLevel: this.zoomLevel,
      width: this.width,
      height: this.height,
      writeToDisk: this.writeToDisk,
      centerCoordinate: this.centerCoordinate,
      bounds: this.bounds,
      withLogo: this.withLogo
    };
  }
  _createCenterCoordPoint(centerCoordinate) {
    const point = (0, _geoUtils.makePoint)(centerCoordinate);
    return (0, _utils.toJSONString)(point);
  }
  _createBoundsCollection(bounds) {
    const features = [];
    for (const bound of bounds) {
      features.push((0, _geoUtils.makePoint)(bound));
    }
    return (0, _utils.toJSONString)((0, _geoUtils.makeFeatureCollection)(features));
  }
}
var _default = exports.default = SnapshotOptions;
//# sourceMappingURL=SnapshotOptions.js.map