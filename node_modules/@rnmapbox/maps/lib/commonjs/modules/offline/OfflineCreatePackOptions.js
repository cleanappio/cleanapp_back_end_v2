"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _geoUtils = require("../../utils/geoUtils");
var _utils = require("../../utils");
class OfflineCreatePackOptions {
  constructor(options) {
    this._assert(options);
    this.name = options.name;
    this.styleURL = options.styleURL;
    this.bounds = this._makeLatLngBounds(options.bounds);
    this.minZoom = options.minZoom;
    this.maxZoom = options.maxZoom;
    this.metadata = this._makeMetadata(options.metadata);
  }
  _assert(options) {
    if (!options.styleURL) {
      throw new Error('Style URL must be provided for creating an offline pack');
    }
    if (!options.name) {
      throw new Error('Name must be provided for creating an offline pack');
    }
    if (!options.bounds) {
      throw new Error('Bounds must be provided for creating an offline pack');
    }
  }
  _makeLatLngBounds(bounds) {
    const [ne, sw] = bounds;
    return (0, _utils.toJSONString)((0, _geoUtils.makeLatLngBounds)(ne, sw));
  }
  _makeMetadata(metadata) {
    return JSON.stringify({
      ...metadata,
      name: this.name
    });
  }
}
var _default = exports.default = OfflineCreatePackOptions;
//# sourceMappingURL=OfflineCreatePackOptions.js.map