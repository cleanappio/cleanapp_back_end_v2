"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = Models;
var _react = _interopRequireDefault(require("react"));
var _reactNative = require("react-native");
var _RNMBXModelsNativeComponent = _interopRequireDefault(require("../specs/RNMBXModelsNativeComponent"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
function _resolveAssets(models) {
  const resolvedModels = {};
  Object.keys(models).forEach(key => {
    const model = models[key];
    if (typeof model === 'string') {
      resolvedModels[key] = {
        url: model
      };
    } else {
      const asset = _reactNative.Image.resolveAssetSource(model);
      if (!asset) {
        throw new Error(`Could not resolve model asset: ${model}`);
      }
      resolvedModels[key] = asset;
    }
  });
  return resolvedModels;
}

/**
 * Name of 3D model assets to be used in the map
 */
function Models(props) {
  const {
    models,
    ...restOfProps
  } = props;
  return /*#__PURE__*/(0, _jsxRuntime.jsx)(_RNMBXModelsNativeComponent.default, {
    ...restOfProps,
    models: _resolveAssets(models)
  });
}
//# sourceMappingURL=Models.js.map