"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _reactNative = require("react-native");
var _ShapeSource = require("../../components/ShapeSource");
var _ImageSource = _interopRequireDefault(require("../../components/ImageSource"));
var _FillLayer = _interopRequireDefault(require("../../components/FillLayer"));
var _FillExtrusionLayer = _interopRequireDefault(require("../../components/FillExtrusionLayer"));
var _LineLayer = _interopRequireDefault(require("../../components/LineLayer"));
var _CircleLayer = _interopRequireDefault(require("../../components/CircleLayer"));
var _SymbolLayer = require("../../components/SymbolLayer");
var _RasterLayer = _interopRequireDefault(require("../../components/RasterLayer"));
var _BackgroundLayer = _interopRequireDefault(require("../../components/BackgroundLayer"));
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const Animated = {
  // sources
  ShapeSource: _reactNative.Animated.createAnimatedComponent(_ShapeSource.ShapeSource),
  ImageSource: _reactNative.Animated.createAnimatedComponent(_ImageSource.default),
  // layers
  FillLayer: _reactNative.Animated.createAnimatedComponent(_FillLayer.default),
  FillExtrusionLayer: _reactNative.Animated.createAnimatedComponent(_FillExtrusionLayer.default),
  LineLayer: _reactNative.Animated.createAnimatedComponent(_LineLayer.default),
  CircleLayer: _reactNative.Animated.createAnimatedComponent(_CircleLayer.default),
  SymbolLayer: _reactNative.Animated.createAnimatedComponent(_SymbolLayer.SymbolLayer),
  RasterLayer: _reactNative.Animated.createAnimatedComponent(_RasterLayer.default),
  BackgroundLayer: _reactNative.Animated.createAnimatedComponent(_BackgroundLayer.default)
};
var _default = exports.default = Animated;
//# sourceMappingURL=Animated.js.map