"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _react = _interopRequireDefault(require("react"));
var _reactNative = require("react-native");
var _RNMBXCalloutNativeComponent = _interopRequireDefault(require("../specs/RNMBXCalloutNativeComponent"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const styles = _reactNative.StyleSheet.create({
  container: {
    alignItems: 'center',
    justifyContent: 'center',
    width: 180,
    zIndex: 9999999
  },
  tip: {
    zIndex: 1000,
    marginTop: -2,
    elevation: 0,
    backgroundColor: 'transparent',
    borderTopWidth: 16,
    borderRightWidth: 8,
    borderBottomWidth: 0,
    borderLeftWidth: 8,
    borderTopColor: 'white',
    borderRightColor: 'transparent',
    borderBottomColor: 'transparent',
    borderLeftColor: 'transparent'
  },
  content: {
    position: 'relative',
    padding: 8,
    flex: 1,
    borderRadius: 3,
    borderWidth: 1,
    borderColor: 'rgba(0, 0, 0, 0.2)',
    backgroundColor: 'white'
  },
  title: {
    color: 'black',
    textAlign: 'center'
  }
});
/**
 *  Callout that displays information about a selected annotation near the annotation.
 */
class Callout extends _react.default.PureComponent {
  get _containerStyle() {
    const style = [{
      position: 'absolute',
      zIndex: 999,
      backgroundColor: 'transparent'
    }];
    if (this.props.containerStyle) {
      style.push(this.props.containerStyle);
    }
    return style;
  }
  get _hasChildren() {
    return _react.default.Children.count(this.props.children) > 0;
  }
  _renderDefaultCallout() {
    return /*#__PURE__*/(0, _jsxRuntime.jsxs)(_reactNative.Animated.View, {
      style: [styles.container, this.props.style],
      children: [/*#__PURE__*/(0, _jsxRuntime.jsx)(_reactNative.View, {
        style: [styles.content, this.props.contentStyle],
        children: /*#__PURE__*/(0, _jsxRuntime.jsx)(_reactNative.Text, {
          style: [styles.title, this.props.textStyle],
          children: this.props.title
        })
      }), /*#__PURE__*/(0, _jsxRuntime.jsx)(_reactNative.View, {
        style: [styles.tip, this.props.tipStyle]
      })]
    });
  }
  _renderCustomCallout() {
    return /*#__PURE__*/(0, _jsxRuntime.jsx)(_reactNative.Animated.View, {
      ...this.props,
      style: this.props.style,
      children: this.props.children
    });
  }
  render() {
    const calloutContent = this._hasChildren ? this._renderCustomCallout() : this._renderDefaultCallout();
    return /*#__PURE__*/(0, _jsxRuntime.jsx)(_RNMBXCalloutNativeComponent.default, {
      style: this._containerStyle,
      children: calloutContent
    });
  }
}
var _default = exports.default = Callout;
//# sourceMappingURL=Callout.js.map