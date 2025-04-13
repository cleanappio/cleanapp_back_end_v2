"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.SymbolLayer = exports.NATIVE_MODULE_NAME = void 0;
var _react = _interopRequireDefault(require("react"));
var _reactNative = require("react-native");
var _RNMBXSymbolLayerNativeComponent = _interopRequireDefault(require("../specs/RNMBXSymbolLayerNativeComponent"));
var _AbstractLayer = _interopRequireDefault(require("./AbstractLayer"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const NATIVE_MODULE_NAME = exports.NATIVE_MODULE_NAME = 'RNMBXSymbolLayer';
const Mapbox = _reactNative.NativeModules.RNMBXModule;

// @{codepart-replace-start(LayerPropsCommon.codepart-tsx)}

// @{codepart-replace-end}

/**
 * SymbolLayer is a style layer that renders icon and text labels at points or along lines on the map.
 */
class SymbolLayer extends _AbstractLayer.default {
  static defaultProps = {
    sourceID: Mapbox.StyleSource.DefaultSourceID
  };
  deprecationLogged = {
    snapshot: false
  };
  _shouldSnapshot() {
    let isSnapshot = false;
    if (_react.default.Children.count(this.baseProps.children) <= 0) {
      return isSnapshot;
    }
    _react.default.Children.forEach(this.baseProps.children, child => {
      if (child?.type === _reactNative.View) {
        isSnapshot = true;
      }
    });
    if (isSnapshot && !this.deprecationLogged.snapshot) {
      console.warn('SymbolLayer: passing children for symbol layer is deprecated, please use @rnmapbox/maps Image component instead. https://github.com/rnmapbox/maps/wiki/Deprecated-SymbolLayerChildren');
      this.deprecationLogged.snapshot = true;
    }
    return isSnapshot;
  }
  render() {
    const props = {
      ...this.baseProps,
      snapshot: this._shouldSnapshot(),
      sourceLayerID: this.props.sourceLayerID
    };
    return (
      /*#__PURE__*/
      // @ts-expect-error just codegen stuff
      (0, _jsxRuntime.jsx)(_RNMBXSymbolLayerNativeComponent.default, {
        ref: this.setNativeLayer,
        ...props,
        children: this.props.children
      })
    );
  }
}
exports.SymbolLayer = SymbolLayer;
//# sourceMappingURL=SymbolLayer.js.map