"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _react = _interopRequireDefault(require("react"));
var _reactNative = require("react-native");
var _RNMBXFillExtrusionLayerNativeComponent = _interopRequireDefault(require("../specs/RNMBXFillExtrusionLayerNativeComponent"));
var _AbstractLayer = _interopRequireDefault(require("./AbstractLayer"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const MapboxGL = _reactNative.NativeModules.RNMBXModule;
/**
 * FillExtrusionLayer is a style layer that renders one or more 3D extruded polygons on the map.
 */
class FillExtrusionLayer extends _AbstractLayer.default {
  static defaultProps = {
    sourceID: MapboxGL.StyleSource.DefaultSourceID
  };
  render() {
    const props = {
      ...this.props,
      ...this.baseProps,
      sourceLayerID: this.props.sourceLayerID
    };
    return /*#__PURE__*/(0, _jsxRuntime.jsx)(_RNMBXFillExtrusionLayerNativeComponent.default
    // @ts-expect-error just codegen stuff
    , {
      ref: this.setNativeLayer,
      ...props
    });
  }
}
var _default = exports.default = FillExtrusionLayer;
//# sourceMappingURL=FillExtrusionLayer.js.map