"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.addToFeatureCollection = addToFeatureCollection;
exports.makeFeature = exports.calculateDistance = void 0;
exports.makeFeatureCollection = makeFeatureCollection;
exports.makeLatLngBounds = makeLatLngBounds;
exports.pointAlongLine = exports.makePoint = exports.makeLineString = void 0;
var _helpers = require("@turf/helpers");
var _distance = _interopRequireDefault(require("@turf/distance"));
var _along = _interopRequireDefault(require("@turf/along"));
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const makePoint = exports.makePoint = _helpers.point;
const makeLineString = exports.makeLineString = _helpers.lineString;
function makeLatLngBounds(northEastCoordinates, southWestCoordinates) {
  return (0, _helpers.featureCollection)([(0, _helpers.point)(northEastCoordinates), (0, _helpers.point)(southWestCoordinates)]);
}
const makeFeature = exports.makeFeature = _helpers.feature;
function makeFeatureCollection(features = [], options) {
  return (0, _helpers.featureCollection)(features, options);
}
function addToFeatureCollection(newFeatureCollection, newFeature) {
  return {
    ...newFeatureCollection,
    features: [...newFeatureCollection.features, newFeature]
  };
}
const calculateDistance = exports.calculateDistance = _distance.default;
const pointAlongLine = exports.pointAlongLine = _along.default;
//# sourceMappingURL=geoUtils.js.map