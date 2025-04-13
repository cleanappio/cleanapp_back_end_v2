"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _react = _interopRequireDefault(require("react"));
var _reactNative = require("react-native");
var _RNMBXSkyLayerNativeComponent = _interopRequireDefault(require("../specs/RNMBXSkyLayerNativeComponent"));
var _AbstractLayer = _interopRequireDefault(require("./AbstractLayer"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const Mapbox = _reactNative.NativeModules.RNMBXModule;
/**
 * SkyLayer is a spherical dome around the map that is always rendered behind all other layers
 */
class SkyLayer extends _AbstractLayer.default {
  static defaultProps = {
    sourceID: Mapbox.StyleSource.DefaultSourceID
  };
  render() {
    return /*#__PURE__*/(0, _jsxRuntime.jsx)(_RNMBXSkyLayerNativeComponent.default
    // @ts-expect-error just codegen stuff
    , {
      ref: this.setNativeLayer,
      ...this.baseProps
    });
  }
}
var _default = exports.default = SkyLayer;
//# sourceMappingURL=SkyLayer.js.map