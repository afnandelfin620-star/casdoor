// Copyright 2026 The Casdoor Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import i18next from "i18next";

export function getSignupErrorMessage(msg) {
  if (msg === "Email already exists" || msg === "该邮箱已存在") {
    return i18next.t("signup:Unable to complete registration. Please sign in or use a different email.");
  }
  if (msg === "Phone already exists" || msg === "该手机号已存在") {
    return i18next.t("signup:Unable to complete registration. Please sign in or use a different phone number.");
  }
  return msg;
}

export function getLoginErrorMessage(msg) {
  if (/^The user: .+ doesn't exist$/.test(msg) || /^用户: .+不存在$/.test(msg) ||
      msg === "the user does not exist, please sign up first" || msg === "用户不存在，请先注册") {
    return i18next.t("login:The user does not exist. Please sign up first.");
  }
  return null;
}
