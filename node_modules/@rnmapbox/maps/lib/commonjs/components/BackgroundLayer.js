"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _react = _interopRequireDefault(require("react"));
var _reactNative = require("react-native");
var _RNMBXBackgroundLayerNativeComponent = _interopRequireDefault(require("../specs/RNMBXBackgroundLayerNativeComponent"));
var _AbstractLayer = _interopRequireDefault(require("./AbstractLayer"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const MapboxGL = _reactNative.NativeModules.RNMBXModule;
class BackgroundLayer extends _AbstractLayer.default {
  static defaultProps = {
    sourceID: MapboxGL.StyleSource.DefaultSourceID
  };
  render() {
    const props = {
      ...this.baseProps,
      sourceLayerID: this.props.sourceLayerID
    };
    return /*#__PURE__*/(0, _jsxRuntime.jsx)(_RNMBXBackgroundLayerNativeComponent.default
    // @ts-expect-error just codegen stuff
    , {
      ref: this.setNativeLayer,
      ...props
    });
  }
}
var _default = exports.default = BackgroundLayer;
//# sourceMappingURL=BackgroundLayer.js.map