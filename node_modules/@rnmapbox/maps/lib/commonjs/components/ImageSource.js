"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = exports.NATIVE_MODULE_NAME = void 0;
var _react = _interopRequireDefault(require("react"));
var _utils = require("../utils");
var _RNMBXImageSourceNativeComponent = _interopRequireDefault(require("../specs/RNMBXImageSourceNativeComponent"));
var _AbstractSource = _interopRequireDefault(require("./AbstractSource"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const NATIVE_MODULE_NAME = exports.NATIVE_MODULE_NAME = 'RNMBXImageSource';
/**
 * ImageSource is a content source that is used for a georeferenced raster image to be shown on the map.
 * The georeferenced image scales and rotates as the user zooms and rotates the map
 */
class ImageSource extends _AbstractSource.default {
  _getURL() {
    const {
      url
    } = this.props;
    if ((0, _utils.isNumber)(url)) {
      return (0, _utils.resolveImagePath)(url);
    } else {
      return url;
    }
  }
  render() {
    if (!this.props.url || !this.props.coordinates || !this.props.coordinates.length) {
      return null;
    }
    const props = {
      ...this.props,
      url: this._getURL()
    };
    return (
      /*#__PURE__*/
      // @ts-expect-error just codegen stuff
      (0, _jsxRuntime.jsx)(_RNMBXImageSourceNativeComponent.default, {
        ref: this.setNativeRef,
        ...props,
        children: (0, _utils.cloneReactChildrenWithProps)(this.props.children, {
          sourceID: this.props.id
        })
      })
    );
  }
}
var _default = exports.default = ImageSource;
//# sourceMappingURL=ImageSource.js.map